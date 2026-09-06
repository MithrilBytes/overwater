package scan

import (
	"fmt"
	"strings"
	"testing"
)

// A prompt that rules a task out must not score as if it asked for it.
func TestNegatedPhrasesDoNotScore(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"plain", "Reply to the customer warmly.", true},
		{"never", "Never reply to the customer.", false},
		{"do not", "Do not reply to the customer.", false},
		{"contraction", "Don't reply to the customer.", false},
		{"avoid", "Avoid reply to the customer entirely.", false},
		// A negation binds to its own clause, not to the next one.
		{"previous sentence", "Never promise refunds. Reply to the customer warmly.", true},
		{"previous clause", "Avoid small talk; reply to the customer warmly.", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := saysAny(strings.ToLower(tt.prompt), []string{"reply to the customer"})
			if got != tt.want {
				t.Errorf("saysAny(%q) = %v, want %v", tt.prompt, got, tt.want)
			}
		})
	}
}

// A helper takes the prompt as an argument, so its own call site says
// nothing about the task and the scorer is left with a token cap and a
// temperature. Every caller says it outright, and layer 5 knows which
// calls those are.
func TestArchetypeCrossesTheFanInEdge(t *testing.T) {
	files := map[string]string{
		"llm.py": `import anthropic

client = anthropic.Anthropic()


def ow_complete(system, prompt, model="claude-opus-5", max_tokens=8, temperature=0):
    return client.messages.create(
        model=model,
        max_tokens=max_tokens,
        temperature=temperature,
        messages=[{"role": "system", "content": system},
                  {"role": "user", "content": prompt}],
    )
`,
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		files[name+".py"] = fmt.Sprintf(`from llm import ow_complete


def route_%s(text):
    return ow_complete("Classify the ticket. Answer with one word: billing, bug, or other.", text)
`, name)
	}
	site := fanInSite(t, analyzeTemp(t, files), "llm.py")
	if site.FanInStatus != FanInExact || site.FanIn != 4 {
		t.Fatalf("fan in = %d (%s), want 4 exact", site.FanIn, site.FanInStatus)
	}
	if site.Archetype != ArchetypeClassification {
		t.Errorf("archetype = %s, want %s: the callers all ask for a label",
			site.Archetype, ArchetypeClassification)
	}
	// The callers' words describe this call by inheritance only.
	if site.ArchetypeConfidence == "high" {
		t.Errorf("archetype confidence = high, want no better than medium for second hand evidence")
	}
}

// A site that named its own task keeps its answer whatever its callers
// are called and whatever they pass.
func TestArchetypeCallersDoNotOverrideTheSite(t *testing.T) {
	files := map[string]string{
		"llm.py": `import anthropic

client = anthropic.Anthropic()


def summarize_release(prompt, model="claude-opus-5", max_tokens=800):
    return client.messages.create(
        model=model,
        max_tokens=max_tokens,
        messages=[{"role": "user", "content": prompt}],
    )
`,
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		files[name+".py"] = fmt.Sprintf(`from llm import summarize_release


def classify_ticket_%s(text):
    return summarize_release("Classify the ticket. Answer with one word: billing, bug, or other.")
`, name)
	}
	site := fanInSite(t, analyzeTemp(t, files), "llm.py")
	if site.FanInStatus != FanInExact {
		t.Fatalf("fan in = %d (%s), want exact", site.FanIn, site.FanInStatus)
	}
	if site.Archetype != ArchetypeSummarization {
		t.Errorf("archetype = %s, want %s: the site named its own task",
			site.Archetype, ArchetypeSummarization)
	}
}

// A prompt that translates into a DSL is asking for code, whatever verb
// it uses and whatever the function is called. The output decides the
// task: a stated code shaped output names codegen and rules translation
// out, while a translation into a human language is untouched.
func TestTranslateIntoADSLIsCodegen(t *testing.T) {
	cases := []struct {
		name, funcName, doc, want string
	}{
		{"dsl", "translate_request_to_dsl",
			"Translate a natural language request into the pipeline DSL, ready for code generation.",
			ArchetypeCodegen},
		{"human language", "translate_menu",
			"Translate the menu into Spanish and keep every placeholder as written.",
			ArchetypeTranslation},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := analyzeTemp(t, map[string]string{"app.py": fmt.Sprintf(`import anthropic

client = anthropic.Anthropic()


def %s(query):
    """%s"""
    return client.messages.create(
        model="claude-opus-5",
        max_tokens=800,
        messages=[{"role": "user", "content": query}],
    )
`, tt.funcName, tt.doc)})
			site := fanInSite(t, r, "app.py")
			if site.Archetype != tt.want {
				t.Errorf("archetype = %s, want %s: the prompt says its output is %s",
					site.Archetype, tt.want, tt.name)
			}
		})
	}
}

// A loop that feeds tool results back to the model is an agent, and
// stays one when a turn's prompt asks for a label: the results coming
// back are the structure, the label is one turn's vocabulary. The same
// call with no result fed back is what its prompt says.
func TestToolLoopIsAgenticWhateverATurnAsks(t *testing.T) {
	const loop = `        if not r.choices[0].message.tool_calls:
            return r
        for call in r.choices[0].message.tool_calls:
            messages.append({"role": "tool", "tool_call_id": call.id, "content": run_tool(call)})
`
	cases := []struct{ name, tail, want string }{
		{"results fed back", loop, ArchetypeAgentic},
		{"one shot", "        return r\n", ArchetypeClassification},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := analyzeTemp(t, map[string]string{"app.py": fmt.Sprintf(`import openai

client = openai.OpenAI()
TOOLS = [{"type": "function", "function": {"name": "lookup_ticket", "parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}}]


def run_rounds(messages):
    for _ in range(5):
        r = client.chat.completions.create(
            model="gpt-4o",
            tools=TOOLS,
            messages=[{"role": "system", "content": "Classify the ticket by its category before you look anything up."}] + messages,
        )
%s    return r
`, tt.tail)})
			site := fanInSite(t, r, "app.py")
			if site.Archetype != tt.want {
				t.Errorf("archetype = %s, want %s", site.Archetype, tt.want)
			}
		})
	}
}

// An image in the input decides the task: a label read off a photo is
// vision, not classification, however plainly the function and the
// prompt ask for the label (corpus/README.md). The same prompt over
// text is classification.
func TestImageInputIsVisionWhateverThePromptAsks(t *testing.T) {
	cases := []struct{ name, content, want string }{
		{"image part", `[
            {"type": "text", "text": "Classify the pictured item as damaged or intact. Answer with one word."},
            {"type": "image_url", "image_url": {"url": image_url}},
        ]`, ArchetypeVision},
		{"text", `"Classify the described item as damaged or intact. Answer with one word.\n" + image_url`,
			ArchetypeClassification},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := analyzeTemp(t, map[string]string{"app.py": fmt.Sprintf(`import openai

client = openai.OpenAI()


def classify_item(image_url):
    return client.chat.completions.create(
        model="gpt-4o",
        messages=[{"role": "user", "content": %s}],
    )
`, tt.content)})
			site := fanInSite(t, r, "app.py")
			if site.Archetype != tt.want {
				t.Errorf("archetype = %s, want %s", site.Archetype, tt.want)
			}
		})
	}
}

// The word tool in an identifier or an error string does not make an
// agent. A call with no tool list, no tools parameter and no result fed
// back is decided by its parameters alone; the same call with tools
// passed by name is agentic.
func TestAgenticNeedsAToolListOrALoop(t *testing.T) {
	cases := []struct{ name, params, want string }{
		{"the word only", "", ArchetypeUnknown},
		{"tools by name", "        tools=TOOLS,\n", ArchetypeAgentic},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := analyzeTemp(t, map[string]string{"app.py": fmt.Sprintf(`import anthropic

client = anthropic.Anthropic()
TOOL_NAME = "propose_response"


def suggest(prompt):
    r = client.messages.create(
        model="claude-sonnet-4-0",
        max_tokens=1024,
%s        messages=[{"role": "user", "content": prompt}],
    )
    if r.content[0].name != TOOL_NAME:
        raise ValueError("the response did not name the expected tool: " + r.content[0].name)
    return r
`, tt.params)})
			site := fanInSite(t, r, "app.py")
			if site.Archetype != tt.want {
				t.Errorf("archetype = %s, want %s", site.Archetype, tt.want)
			}
		})
	}
}

// A prompt written in Russian, Chinese or Korean names its task as
// plainly as an English one, and used to be a dead end for the word
// lists: the scorer was left with the parameters and answered unknown.
// The two block literal cases need the masker as well, since a Ruby
// heredoc and a Java text block used to scan as code, not as the prompt.
func TestNonEnglishPromptsNameTheTask(t *testing.T) {
	const python = `import anthropic

client = anthropic.Anthropic()


def run(text):
    return client.messages.create(
        model="claude-opus-5",
        max_tokens=800,
        messages=[{"role": "user", "content": "%s" + text}],
    )
`
	cases := []struct{ name, file, content, want string }{
		{"russian summary", "app.py",
			fmt.Sprintf(python, "Суммируй основные претензии и пожелания студентов. Предложи как поменять урок."),
			ArchetypeSummarization},
		// "не" flips the phrase after it the way "never" does.
		{"russian negation", "app.py",
			fmt.Sprintf(python, "Не переводи текст пользователя. Отвечай на языке пользователя, коротко и дружелюбно."),
			ArchetypeChat},
		{"chinese extraction", "app.py",
			fmt.Sprintf(python, "请从下面的文本抽取一个或多个四元组，每一个四元组输出格式为评论对象|对象观点|仇恨群体|是否仇恨。"),
			ArchetypeExtraction},
		{"chinese label from a list", "app.py",
			fmt.Sprintf(python, "请根据数据推荐一个合适的图表, 只需要给出图表类型的英文名称。可选择的图表类型如下：Area, Line, Bar, Radar"),
			ArchetypeClassification},
		{"korean classification", "app.py",
			fmt.Sprintf(python, "사용자 메시지의 의도를 분류하세요. 반드시 위 분류 중 하나만 응답하세요."),
			ArchetypeClassification},
		{"korean instruction to extract", "app.py",
			fmt.Sprintf(python, "다음 문장에서 사람 이름과 날짜를 추출하세요. 결과는 JSON 배열로 반환하세요."),
			ArchetypeExtraction},
		// The noun in a description of a flow is a step, not the task the
		// call is for, so it names nothing.
		{"korean extraction as a step", "app.py",
			fmt.Sprintf(python, "사용자 메시지에서 메모 내용을 추출하고 적절한 폴더에 저장하는 흐름을 처리한다."),
			ArchetypeUnknown},
		{"ruby heredoc", "review_job.rb", `RubyLLM.configure do |config|
  config.default_model = "gpt-4.1"
end

class ReviewJob < ApplicationJob
  def perform(lesson, questions)
    instructions = <<~PROMPT
      Суммируй основные претензии и пожелания студентов по уроку «#{lesson.name}».
      Предложи как поменять урок.
    PROMPT
    RubyLLM.chat.with_instructions(instructions).ask(questions)
  end
end
`, ArchetypeSummarization},
		{"java text block", "DashboardPicker.java", `import org.springframework.ai.chat.client.ChatClient;
import org.springframework.ai.openai.OpenAiChatOptions;

public class DashboardPicker {
    private final ChatClient chatClient;

    public DashboardPicker(ChatClient.Builder builder) {
        this.chatClient = builder
                .defaultOptions(OpenAiChatOptions.builder().model("gpt-4o-mini").build())
                .build();
    }

    public String pick(String data) {
        String template = """
                请根据数据推荐一个合适的图表, 只需要给出图表类型的英文名称。
                可选择的图表类型如下：Area, Line, Bar, Radar
                """;
        return chatClient.prompt().user(template + data).call().content();
    }
}
`, ArchetypeClassification},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			site := fanInSite(t, analyzeTemp(t, map[string]string{tt.file: tt.content}), tt.file)
			if site.Archetype != tt.want {
				t.Errorf("archetype = %s (%s), want %s", site.Archetype, site.ArchetypeConfidence, tt.want)
			}
		})
	}
}

// Two real cases where a single label was read as its neighbour. A
// strict grouping prompt says "sort the entries into the following
// categories", and reranking's "sort the" outvoted classification's
// "categor"; a quality gate answers "is_good_quality: boolean (true if
// ...)" and extraction won on "text extracted from a PDF", the input's
// provenance rather than the task.
func TestASingleLabelIsNotItsNeighbour(t *testing.T) {
	for _, tc := range []struct {
		name, file, src, want string
	}{
		{"assignment to one category", "group.go", `package d

import "context"

const strictGroupingPrompt = "You are organizing a digest into specific categories.\n" +
	"Sort the entries below into the following categories: {{.}}\n" +
	"You MUST use ONLY the categories listed above. Do NOT create new categories.\n" +
	"An entry can only belong to one group."

func group(ctx context.Context, c *Client) {
	c.Generate(ctx, "gemini-2.5-flash", strictGroupingPrompt)
}
`, ArchetypeClassification},
		{"the sibling digest is still a summary", "summary.go", `package d

import "context"

const summaryPrompt = "You are an expert at summarizing content for busy readers. You will be given " +
	"a list of entries from a primary group. Write a concise, 2-3 sentence summary that gives " +
	"a high-level overview of the main themes and highlights the most significant entries."

func summarize(ctx context.Context, c *Client) {
	c.Generate(ctx, "gemini-2.5-flash", summaryPrompt)
}
`, ArchetypeSummarization},
		{"a boolean verdict", "gate.py", `from openai import OpenAI

client = OpenAI()

def check(sample):
    return client.chat.completions.create(model="gpt-4o-mini", temperature=0,
        messages=[
            {"role": "system", "content": "You are a document quality assessor. Respond only with valid JSON."},
            {"role": "user", "content": "Evaluate whether the text extracted from a PDF is high-quality, "
                "or whether it looks like garbled OCR output. Return a JSON object with exactly these fields: "
                '"quality_score": integer 0-100; "is_good_quality": boolean (true if quality_score >= 85); '
                '"feedback": one-sentence summary of your assessment. Text: ' + sample},
        ])
`, ArchetypeClassification},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, map[string]string{tc.file: tc.src, "requirements.txt": "openai==1.0\n", "go.mod": "module d\n"})
			report, err := Analyze(dir, mustCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Sites) != 1 {
				t.Fatalf("sites = %d, want 1: %+v", len(report.Sites), report.Sites)
			}
			if got := report.Sites[0].Archetype; got != tc.want {
				t.Errorf("archetype = %s, want %s", got, tc.want)
			}
		})
	}
}
