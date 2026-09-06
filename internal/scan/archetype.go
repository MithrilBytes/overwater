package scan

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Archetypes layer 4 can assign to a call site.
const (
	ArchetypeEmbedding      = "embedding"
	ArchetypeClassification = "classification"
	ArchetypeExtraction     = "extraction"
	ArchetypeSummarization  = "summarization"
	ArchetypeAgentic        = "agentic"
	ArchetypeChat           = "chat"
	ArchetypeTranslation    = "translation"
	ArchetypeReranking      = "reranking"
	ArchetypeModeration     = "moderation"
	ArchetypeTranscription  = "transcription"
	ArchetypeVision         = "vision"
	ArchetypeCodegen        = "codegen"
	ArchetypeUnknown        = "unknown"
)

// archetypePriority breaks score ties deterministically; the narrower
// task classes come before the broad ones.
var archetypePriority = []string{
	ArchetypeModeration,
	ArchetypeReranking,
	ArchetypeTranslation,
	ArchetypeTranscription,
	ArchetypeVision,
	ArchetypeClassification,
	ArchetypeExtraction,
	ArchetypeSummarization,
	ArchetypeCodegen,
	ArchetypeAgentic,
	ArchetypeChat,
}

// Scoring weights. Every number the scorer uses lives in this block.
const (
	// What the call asks the model to do.
	weightSays     = 7  // the prompt names the task outright
	weightFuncName = 5  // the enclosing function name
	weightHint     = 3  // the prompt leans this way
	weightCode     = 2  // an identifier near the call
	weightDenied   = -5 // the prompt rules this task out in its own words

	// What the call looks like.
	weightEndpoint     = 6  // the SDK method or content part that was called
	weightShapeStrong  = 4  // a schema or a token cap that only one task fits
	weightShapeWeak    = 2  // a parameter that leans one way
	weightShapeHint    = 1  // a parameter that barely leans
	weightShapeAgainst = -4 // a parameter this task would not be written with
	// A tool result fed back to the model is the loop that makes an
	// agent, and a loop with tools is agentic whatever one turn's prompt
	// asks for: the content part fed back, plus the tool list a loop
	// implies. Above weightSays plus weightHint, which is the most a
	// prompt can say for one of the one shot tasks.
	weightLoop = weightEndpoint + weightShapeStrong + weightShapeWeak
)

// Token caps read as intent: capLabel fits a label or a boolean,
// capLong fits prose, a document, or a program.
const (
	capLabel = 24
	capShort = 200
	capLong  = 1500
)

// Score and margin a winner needs. Below scoreFloor the answer is
// unknown, which the rules read as doubt rather than as a wrong answer.
const (
	scoreFloor   = 4
	scoreHigh    = 9
	marginHigh   = 4
	scoreMedium  = 5
	marginMedium = 2
)

// archetypeKeywords is one archetype's evidence table.
//
//	idents  identifier stems, matched against the enclosing function
//	        name and against the code around the call
//	says    phrases that name the task outright, matched against the
//	        prompt text the call sends
//	hints   phrases that lean that way without naming the task
//	denies  phrases the prompt uses to rule the task out; a match both
//	        scores against the archetype and suppresses its says and
//	        hints, so "do not translate the text" is not translation
type archetypeKeywords struct {
	archetype string
	idents    []string
	says      []string
	hints     []string
	denies    []string
}

// codeOutputs are the ways a prompt says its output is code without
// saying "code": a DSL, SQL, a script. They are shared between codegen,
// which they name, and translation, which they rule out, because
// "translate the request into the DSL" is a codegen prompt that happens
// to use translation's verb. The output decides the task, not the verb.
var codeOutputs = []string{"dsl", "into code", "into sql", "to sql", "into a script", "code generation"}

// Prompts are written in the language of the people who read the
// output, and a prompt the word lists cannot read leaves the scorer with
// the call's parameters alone, which is the unknown verdict for most real
// sites. So each list carries, after its English, the phrases that name
// the task in Russian, Chinese and Korean: the imperative and the noun a
// prompt in that language uses. The idents lists stay English, since
// code is.
//
// Korean marks an instruction in the verb ending, and extraction is the
// task an agent's prompt most often describes as a step ("extract the
// memo, then save it"), so its Korean entries are the imperative forms:
// the bare noun in a description of a flow is not what the call is for.
// A phrase Japanese shares with Chinese is left out, since the lists do
// not otherwise read Japanese and a lone match would decide the site.
var archetypeWords = []archetypeKeywords{
	{
		archetype: ArchetypeClassification,
		idents:    []string{"classif", "categor", "triage", "sentiment", "intent", "priority", "churn", "grade", "detect"},
		says: []string{"classif", "categor", "triage", "pick one", "choose one", "choose the single",
			"into the following categories", "into specific categories", "belong to one group",
			"single label", "one label", "exactly one word", "answer with one word", "one word:",
			"which queue", "assign the", "label the", "tag the", "from the allowed list",
			"one of the following", "answer with the label", "answer with the code",
			"классифи", "категори", "определи тип", "выбери один", "выбери одну",
			"один из следующих", "одно из следующих", "одну из следующих",
			"分类", "归类", "类别", "选择一个", "选出一个", "以下之一", "其中之一", "可选择的",
			"분류", "카테고리"},
		hints: []string{"evaluate whether", "assess whether", "determine whether", "decide whether",
			"judge whether", "criteria for", "quality assessor",
			"label", "category", "intent", "priority", "urgency", "sentiment", "topic",
			"risk", "iso code", "one word", "one of:",
			"метк", "настроени", "намерени", "приоритет",
			"标签", "意图", "情感", "优先级",
			"의도", "라벨", "레이블", "감정", "우선순위"},
	},
	{
		archetype: ArchetypeExtraction,
		idents:    []string{"extract", "parse", "fields", "invoice", "receipt", "resume", "purchase_order"},
		says: []string{"extract", "copy the", "copied from", "pull the", "read out the", "record the fields",
			"return json with the keys", "list each commitment", "fill in the fields", "copy values",
			"извле", "заполни поля",
			"提取", "抽取",
			"추출하세요", "추출해", "추출하라", "추출하시오"},
		hints: []string{"json with", "fields", "do not guess", "never infer", "as written", "null when",
			"leave a field empty",
			"поля", "字段", "필드"},
	},
	{
		archetype: ArchetypeSummarization,
		idents:    []string{"summar", "digest", "recap", "rollup", "roll_up", "tldr", "condense", "brief"},
		says: []string{"summar", "sum up", "sums up", "condense", "tl;dr", "digest", "recap",
			"release notes", "meeting notes", "key points", "main points", "brief on", "brief for",
			"суммируй", "резюмируй", "кратко излож", "краткое содержание", "краткий пересказ",
			"основные тезисы", "ключевые моменты",
			"总结", "摘要", "概括", "概述",
			"요약"},
		hints: []string{"paragraph", "sentences", "bullets", "lead with", "plain language", "skip",
			"абзац", "предложени", "段落", "문단"},
		denies: append([]string{"do not summarize", "no summary", "not a summary", "do not condense", "do not shorten"},
			assignsToOneCategory...),
	},
	{
		archetype: ArchetypeChat,
		idents:    []string{"chat", "reply", "respond", "conversation", "companion", "concierge", "helpdesk"},
		says: []string{"stay in character", "keep the conversation", "ask a follow up", "ask one question",
			"answer the customer", "answer the user", "answer the reader", "answer visitors", "answer the caller",
			"reply to the customer", "reply to the user", "respond to the customer", "respond to the user",
			"keep replies", "keep each reply", "chat naturally", "conversational", "talk the", "reply in",
			"end with a question", "keep the tone", "in a friendly voice",
			"отвечай на языке", "отвечай пользователю", "отвечай на вопрос", "веди диалог",
			"поддерживай разговор", "общайся",
			"回答用户", "回复用户", "与用户对话",
			"사용자에게 답변", "대화를 이어", "친절하게 답변"},
		hints: []string{"you are a", "you are the", "assistant", "friendly", "warm", "brief", "chat",
			"conversation", "in character", "buddy",
			"ассистент", "разговор", "диалог", "ты помогаешь", "дружелюбн", "собеседник",
			"你是一", "您是一", "助手", "对话", "友好", "聊天",
			"당신은", "어시스턴트", "대화", "친절"},
		denies: []string{"do not answer", "no commentary", "nothing else", "output only", "no explanation",
			"do not reply", "numbers only", "one word", "no prose",
			"ничего больше", "без пояснений", "без объяснений", "одним словом", "только json", "только число",
			"只输出", "仅输出", "只返回", "仅返回", "不要解释", "无需解释", "一个词", "一个单词",
			"만 출력", "만 응답", "만 답변", "설명 없이", "한 단어"},
	},
	{
		archetype: ArchetypeTranslation,
		idents:    []string{"translat", "localiz", "locale", "toenglish", "to_english"},
		says: []string{"translat", "localiz", "target language", "target locale", "in english", "into english",
			"into the recipient", "into the language", "into their language",
			"перевод", "переведи", "перевести", "переводи", "на английский", "на русский",
			"翻译", "译成", "译为", "目标语言",
			"번역", "대상 언어"},
		hints:  []string{"locale", "placeholder", "in the target"},
		denies: append([]string{"do not translate", "never translate"}, codeOutputs...),
	},
	{
		archetype: ArchetypeReranking,
		idents:    []string{"rerank", "rank", "reorder", "relevance", "order_", "_order"},
		denies:    assignsToOneCategory,
		says: []string{"rerank", "rank the", "reorder", "order the", "sort the", "by relevance",
			"most relevant", "best first", "in order of", "ordered by", "descending score", "relevance to",
			"отсортир", "ранжир", "упорядоч", "в порядке", "по релевантности", "наиболее релевантн",
			"наиболее подходящ", "по убыванию", "самые подходящие",
			"排序", "重排", "按相关", "最相关", "从高到低",
			"순위", "정렬", "관련성 순", "가장 관련"},
		hints: []string{"relevance", "ranking", "candidates", "passages", "in order",
			"релевантн", "кандидат", "по похожести",
			"相关性", "候选", "相似度",
			"관련성", "후보"},
	},
	{
		archetype: ArchetypeModeration,
		idents:    []string{"moderat", "gate", "guard", "policy", "safety", "abuse", "unsafe", "blocked"},
		says: []string{"moderat", "policy", "allow or block", "answer allow", "safe or unsafe",
			"community guidelines", "brand safe", "violat", "harassment", "screen the", "screens",
			"approve or remove", "reject listings", "block ",
			"модерац", "нарушает", "нарушени",
			"违规", "违反", "是否安全", "内容审核",
			"위반"},
		hints: []string{"flag", "abusive", "spam", "unsafe", "filter", "forbid", "guideline",
			"спам", "токсичн", "оскорб", "запрещ",
			"垃圾信息", "辱骂", "有害",
			"스팸", "유해", "욕설"},
	},
	// The stem is transcrib, not transcri: transcripts usually names a
	// summarizer's input, not this task.
	{
		archetype: ArchetypeTranscription,
		idents:    []string{"transcrib", "transcription", "whisper", "speech_to_text", "dictation", "stt"},
		says: []string{"transcrib", "word for word", "verbatim", "speech to text", "write out everything",
			"exactly what the", "what the caller says",
			"транскриб", "транскрипц", "расшифруй", "расшифровк", "дословно",
			"转录", "转写", "逐字", "语音转文字", "语音识别",
			"음성을 텍스트로", "받아쓰"},
		hints: []string{"speaker", "filler words", "false starts", "timestamp",
			"говорящ", "说话人", "时间戳", "화자", "타임스탬프"},
	},
	{
		archetype: ArchetypeVision,
		idents:    []string{"vision", "ocr", "image", "photo", "screenshot", "chart", "slide", "visual"},
		says: []string{"in this image", "in the image", "this photo", "this picture", "this screenshot",
			"ocr", "reading order", "what is in this", "visible in", "see in this", "changed visually",
			"printed on them", "plotted here",
			"на изображении", "на фото", "на картинке", "на скриншоте", "на снимке",
			"图片中", "图像中", "照片中", "截图中", "这张图", "图中",
			"이미지에서", "사진에서", "이 이미지", "이 사진", "스크린샷에서"},
		hints: []string{"image", "photo", "screenshot", "slide", "chart", "visually", "illegible",
			"изображени", "фото", "скриншот", "картинк",
			"图片", "图像", "照片", "截图",
			"이미지", "사진", "스크린샷"},
	},
	{
		archetype: ArchetypeCodegen,
		idents: []string{"codegen", "write_code", "generate_code", "writecode", "generatecode", "autocomplete",
			"sql", "migration", "scaffold", "regex", "stub", "fim", "patch"},
		says: append([]string{"write code", "generate code", "unit test", "pytest", "sql query", "output only sql", "the script",
			"shell command", "command template", "generate commands", "command line", "cli assistant",
			"regular expression", "bash script", "terraform", "hcl only", "code only", "source only",
			"compilable", "no diff markers", "jq filter", "migration sql", "emit ",
			"напиши код", "сгенерируй код", "напиши скрипт", "напиши функцию", "sql-запрос", "sql запрос", "только sql",
			"生成代码", "编写代码", "写代码", "sql语句", "sql 语句", "查询语句", "生成sql", "生成 sql",
			"코드를 작성", "코드 생성", "코드를 생성", "sql 쿼리", "쿼리를 작성"}, codeOutputs...),
		hints: []string{"sql", "code", "syntax", "controller", "module",
			"скрипт", "代码", "脚本", "코드", "스크립트"},
	},
	{
		archetype: ArchetypeAgentic,
		idents:    []string{"agent", "scratchpad", "tool_", "toolconfig", "tools", "runner", "next_step", "nextstep"},
		says: []string{"one tool call at a time", "take one action", "keep going until", "until you can",
			"decide which tool", "use the tools", "keep querying", "plan, act", "and verify before",
			"stop when", "then stop", "agent",
			"агент", "智能体", "에이전트",
			"используй инструменты", "вызови инструмент", "вызывай инструменты", "доступные инструменты",
			"подходящий инструмент", "вызов инструмент",
			"调用工具", "使用工具", "工具调用", "可用的工具", "合适的工具", "调用函数",
			"도구를 사용", "도구를 호출", "도구를 활용", "도구 호출", "적절한 도구", "필요한 도구",
			"사용 가능한 도구", "툴을 사용", "툴 호출"},
		hints: []string{"tool", "step", "plan", "loop",
			"инструмент", "工具", "도구", "툴"},
	},
}

// Endpoints and content parts. Scored above any keyword: the SDK method
// a call reaches for names the task outright.
var endpointSignals = []struct {
	marker    string
	archetype string
}{
	{"moderations", ArchetypeModeration},
	{"audio.transcriptions", ArchetypeTranscription},
	{"audio.transcription", ArchetypeTranscription},
	{"transcriptions.create", ArchetypeTranscription},
	{"transcribeaudio", ArchetypeTranscription},
	{"transcribe", ArchetypeTranscription},
	{"input_audio", ArchetypeTranscription},
	{"audio/", ArchetypeTranscription},
	{".rerank", ArchetypeReranking},
	{"fim.complete", ArchetypeCodegen},
}

// Image content parts: the part's type or key, a MIME prefix, or an
// SDK constructor. Read from the window around the call and not only
// from its argument list, the way a forced tool is, because the
// message list that carries the image is often built away from the
// call: in a helper above it, or below a model constant. That reach is
// why image_url needs its quotes or the colon of a key: as a bare
// identifier it is a parameter name as often as a part.
var reImagePart = regexp.MustCompile(`["']image_?url["']|image_?url\s*[:=(]|image/|inline_?data|imagecontentpart|createimagepart|ofimage`)

// An image in the input rules out the readings that would take the
// prompt's words at face value: a label, a set of fields or a digest
// read off a photo is vision (corpus/README.md, the labeling rule).
var imageInputDenies = map[string]bool{
	ArchetypeClassification: true,
	ArchetypeExtraction:     true,
	ArchetypeSummarization:  true,
}

// A tools parameter spelled without an inline list: keyed, as a PHP
// arrow, as a shorthand property, passed to or returned from a call
// (bind_tools, setTools, Tools.Add), added one at a time by a builder
// (addTool, toolCallbacks), or as a config object. The word tool in an
// identifier or an error string is not one; agentic needs the
// structure, not the word.
var reToolsParam = regexp.MustCompile(`tools["']?\??\s*[:=,})(]|tools\.add\(|add_?tools?\(|tool_?callbacks?\s*[:=(]|tool_?call_?behavior\s*=|\btool_?config\s*[:=]`)

// An archetype pragma pins a call site the heuristics get wrong:
//
//	// overwater:archetype=extraction
var rePragma = regexp.MustCompile(`overwater:archetype=([a-z]+)`)

func validArchetype(s string) bool {
	switch s {
	case ArchetypeEmbedding, ArchetypeClassification, ArchetypeExtraction,
		ArchetypeSummarization, ArchetypeAgentic, ArchetypeChat,
		ArchetypeTranslation, ArchetypeReranking, ArchetypeModeration,
		ArchetypeTranscription, ArchetypeVision, ArchetypeCodegen:
		return true
	}
	return false
}

// classify returns the archetype the call site's evidence favours, with
// a graded confidence. A pragma in or just above the region, and an
// embedding call, both win outright; everything else is scored.
func (a *analyzer) classify(p string, shape Shape, r region, tier string) (string, string) {
	arch, conf, _ := a.classifySite(p, shape, r, tier)
	return arch, conf
}

// classifySite is classify, and also reports whether anything at the
// site named the task. A site that named nothing was decided by its
// parameters alone, which is the wrapper case layer 5 comes back to
// (archetypeFromCallers).
func (a *analyzer) classifySite(p string, shape Shape, r region, tier string) (string, string, bool) {
	content := a.byPath[p]
	pragmaStart := linesAbove(content, r.start, 3)
	if m := rePragma.FindStringSubmatch(content[pragmaStart:r.end]); m != nil && validArchetype(m[1]) {
		return m[1], "high", true
	}
	if shape.EmbeddingCall || tier == "embedding" {
		return ArchetypeEmbedding, "high", true
	}
	narrow := a.archetypeScores(p, shape, r)
	if arch, conf, ok := narrow.winner(); ok {
		return arch, conf, true
	}
	// The region names no task, which is ordinary: the prompt is often a
	// constant at the top of the file. Widen, and downgrade the answer,
	// since the call site did not make it on its own.
	window := region{start: max(0, r.hit-fileWindowBytes), end: min(len(content), r.hit+fileWindowBytes), hit: r.hit}
	wide := a.archetypeScores(p, shape, window)
	if arch, conf, ok := wide.winner(); ok {
		if conf == "high" {
			conf = "medium"
		}
		return arch, conf, true
	}
	// Neither pass found a word about the task, so the parameters are all
	// that is left. Reported low.
	arch, _ := rank(narrow.scores)
	return arch, "low", false
}

// How much of the caller set one site reads: enough calls to see what a
// helper is for, bounded so a helper reached from hundreds of places
// does not turn one classification into a repository wide read.
const (
	callerEvidenceMax   = 24
	callerEvidenceBytes = 16000
)

// archetypeFromCallers re-reads a wrapper's archetype with what the
// callers ask for. A helper that takes the prompt as an argument says
// nothing about the task at its own call, so the scorer is left with a
// token cap and a temperature; every caller says it outright, and layer
// 5 knows exactly which calls those are. Second hand evidence, so it
// never answers high and never overrides a site that named its own
// task.
func (a *analyzer) archetypeFromCallers(idx *repoIndex, s *Site, tier string, callers []callRef) {
	r := a.regionFor(s.File, s.Line, s.Col)
	if _, _, named := a.classifySite(s.File, s.Shape, r, tier); named {
		return
	}
	prompt, funcNames := a.callerEvidence(idx, callers)
	if prompt == "" && funcNames == "" {
		return
	}
	ev := a.evidenceFor(s.File, s.Shape, r)
	ev.prompt += "\n" + prompt
	ev.funcName += "\n" + funcNames
	arch, conf, ok := scoreEvidence(s.Shape, ev).winner()
	if !ok {
		return
	}
	if conf == "high" {
		conf = "medium"
	}
	s.Archetype, s.ArchetypeConfidence = arch, conf
}

// callerEvidence joins what a wrapper's callers write: the literals in
// the argument list, which is where the prompt is passed, and the name
// of the function each call sits in. Both are read the way evidenceFor
// reads them at a site, so a caller's prompt scores as a prompt and its
// function name as a name.
func (a *analyzer) callerEvidence(idx *repoIndex, callers []callRef) (string, string) {
	var prompt, funcNames strings.Builder
	for i, c := range callers {
		if i >= callerEvidenceMax || prompt.Len() > callerEvidenceBytes {
			break
		}
		all := a.masked(c.file).all
		if c.open >= len(all) {
			continue
		}
		closer, ok := matchClose(all, c.open)
		if !ok {
			continue
		}
		if lits := a.regionLiterals(c.file, region{start: c.open, end: closer + 1, hit: c.open}); lits != "" {
			prompt.WriteString(strings.ToLower(lits))
			prompt.WriteByte('\n')
		}
		if d := idx.enclosing(c.file, c.open); d != nil {
			funcNames.WriteString(strings.ToLower(d.name))
			funcNames.WriteByte('\n')
		}
	}
	return prompt.String(), funcNames.String()
}

// How far the widened pass reads either side of the reference. Wide
// enough for a prompt constant at the top of a file, bounded so a
// minified config does not become a whole file scan per reference.
const fileWindowBytes = 4000

// evidence is what the scorer reads about one call site. The fields stay
// apart so prose cannot be read as code or the reverse.
type evidence struct {
	// idents holds code with every string blanked: identifiers only, so
	// a prose fragment like "Triage notes: " cannot pose as code.
	idents string
	// markers holds code with only long strings blanked, so syntax level
	// strings like "image_url" survive while prompts do not.
	markers string
	// prompt holds what the call sends the model: the resolved system
	// prompt plus the string literals written in the call.
	prompt   string
	funcName string
	// forcedTool is true when a tool choice names one tool, which the
	// call often spells in the config object it points at rather than
	// in the call itself.
	forcedTool bool
	// imageInput is true when an image content part is in the call or
	// in the window around it.
	imageInput bool
	// toolLoop is true when the file feeds a tool result back to the
	// model. Read off the whole file (fileFacts): the loop and the
	// model constant it drives rarely share a region.
	toolLoop bool
}

func (a *analyzer) evidenceFor(p string, shape Shape, r region) evidence {
	src := a.masked(p)
	idents := strings.ToLower(src.all[r.start:r.end])
	// The prompt's content already scores as prompt evidence; counting
	// the name of the variable that holds it would count it twice.
	for _, ident := range promptIdents(src.prose[r.start:r.end]) {
		idents = strings.ReplaceAll(idents, strings.ToLower(ident), " ")
	}
	// The enclosing function is the last definition before the model
	// string, not before the head expanded region start.
	funcName := strings.ToLower(enclosingFuncName(src.prose, r.hit))
	// Its definition sits inside the region too, and scoring the same
	// name as both the function and a nearby identifier doubles it.
	if funcName != "" {
		idents = strings.ReplaceAll(idents, funcName, " ")
	}
	// SDK paths name the transport, not the task: every OpenAI style
	// call reads chat.completions whatever it is asking for.
	for _, path := range sdkPaths {
		idents = strings.ReplaceAll(idents, path, " ")
	}
	prompt := strings.ToLower(shape.SystemPromptText)
	if lits := a.regionLiterals(p, r); lits != "" {
		prompt += "\n" + strings.ToLower(lits)
	}
	// The window is where a tool choice or an image part could be
	// written; it is the widest read this function does.
	window := src.prose[max(0, r.hit-fileWindowBytes):min(len(src.prose), r.hit+fileWindowBytes)]
	forced := shape.ForcedTool
	if !forced {
		forced = strings.Contains(window, "hoice") && reForcedTool.MatchString(window)
	}
	markers := strings.ToLower(src.prose[r.start:r.end])
	// The region first, since an extent can outrun the window.
	image := reImagePart.MatchString(markers) || reImagePart.MatchString(strings.ToLower(window))
	return evidence{
		idents:     idents,
		markers:    markers,
		prompt:     prompt,
		funcName:   funcName,
		forcedTool: forced,
		imageInput: image,
		toolLoop:   a.facts(p).toolLoop,
	}
}

var sdkPaths = []string{
	"chat.completions", "chats.create", "chat.complete", "chat.stream", "chat()",
	"chatcompletion", "chatclient", "chatmessage", "chatmodel", "chatrequest", "chatanthropic", "chatopenai",
}

// regionLiterals joins the string literals written inside the region:
// the instruction sits in a user message or a template as often as in
// the system prompt. Literals under promptLiteralMinBytes are skipped,
// so a config value like "summarizer-worker" is not read as one.
const (
	regionLiteralsMaxBytes = 8000
	promptLiteralMinBytes  = 30
)

func (a *analyzer) regionLiterals(p string, r region) string {
	content := a.byPath[p]
	spans := a.spans(p)
	first := sort.Search(len(spans), func(i int) bool { return spans[i].end > r.start })
	var b strings.Builder
	for _, s := range spans[first:] {
		if s.start >= r.end {
			break
		}
		if s.kind != spanString {
			continue
		}
		lo, hi := max(s.interiorStart, r.start), min(s.interiorEnd, r.end)
		if hi-lo < promptLiteralMinBytes {
			continue
		}
		b.WriteString(content[lo:hi])
		b.WriteByte('\n')
		if b.Len() > regionLiteralsMaxBytes {
			break
		}
	}
	return b.String()
}

// scoreSet is one pass of the scorer. named records, per archetype,
// whether anything named the task for it: a function name, a phrase in
// the prompt, an endpoint, an output schema. A winner with nothing named
// was picked by a token cap and a temperature alone.
type scoreSet struct {
	scores map[string]int
	named  map[string]bool
}

// winner returns the archetype this pass names, if it names one.
func (s scoreSet) winner() (string, string, bool) {
	arch, conf := rank(s.scores)
	return arch, conf, arch != ArchetypeUnknown && s.named[arch]
}

// archetypeScores weighs every archetype against the evidence around
// the call: what the prompt asks for, what the enclosing function is
// called, which endpoint was reached for, and the shape of the call.
func (a *analyzer) archetypeScores(p string, shape Shape, r region) scoreSet {
	return scoreEvidence(shape, a.evidenceFor(p, shape, r))
}

// scoreEvidence is the scorer proper, apart from the gather so a site
// can be scored a second time with evidence its own file does not hold
// (archetypeFromCallers).
func scoreEvidence(shape Shape, ev evidence) scoreSet {
	set := scoreSet{scores: map[string]int{}, named: map[string]bool{}}
	// names is false for a stem that only turned up in a nearby
	// identifier: that is corroboration, not a statement of the task.
	add := func(arch string, points int, names bool) {
		set.scores[arch] += points
		set.named[arch] = set.named[arch] || names
	}

	verdict := verdictOutput(ev.prompt)
	for _, fam := range archetypeWords {
		if containsAny(ev.funcName, fam.idents) {
			add(fam.archetype, weightFuncName, true)
		}
		if containsAny(ev.idents, fam.idents) {
			add(fam.archetype, weightCode, false)
		}
		// The input rules a task out the way the prompt's own words
		// can, and the words are then about the photo.
		if ev.imageInput && imageInputDenies[fam.archetype] {
			add(fam.archetype, weightDenied, true)
			continue
		}
		// So does the output: an answer that is a verdict is not a
		// record of fields and not a digest, whatever the prompt calls
		// its input.
		if verdict && verdictDenies[fam.archetype] {
			add(fam.archetype, weightDenied, true)
			continue
		}
		if ev.prompt == "" {
			continue
		}
		if containsAny(ev.prompt, fam.denies) {
			add(fam.archetype, weightDenied, true)
			continue
		}
		if saysAny(ev.prompt, fam.says) {
			add(fam.archetype, weightSays, true)
		}
		if saysAny(ev.prompt, fam.hints) {
			add(fam.archetype, weightHint, true)
		}
	}
	for _, sig := range endpointSignals {
		if strings.Contains(ev.markers, sig.marker) {
			add(sig.archetype, weightEndpoint, true)
		}
	}
	if ev.imageInput {
		add(ArchetypeVision, weightEndpoint, true)
	}
	shapeScores(add, shape, ev)
	return set
}

// shapeScores reads the call's parameters: the output schema, the room
// the answer was given, and whether the call samples or decides.
// A schema and a tool list are parsed parameters and name a task; a
// token cap or a temperature only leans.
func shapeScores(add func(arch string, points int, names bool), shape Shape, ev evidence) {
	scores := func(arch string, points int) { add(arch, points, false) }
	if shape.SchemaEnumOnly || enumOnlyOutput(ev.markers) || verdictOutput(ev.prompt) {
		add(ArchetypeClassification, weightShapeStrong, true)
	}
	if shape.SchemaMultiField {
		add(ArchetypeExtraction, weightShapeStrong, true)
	}
	if singleBooleanSchema(ev.markers) {
		add(ArchetypeModeration, weightShapeStrong, true)
	}
	if shape.JSONSchema {
		scores(ArchetypeExtraction, weightShapeHint)
		scores(ArchetypeChat, weightShapeAgainst)
	}
	if ev.forcedTool {
		// A forced tool is a schema by another name: the model fills the
		// fields in, it does not choose what to do next.
		add(ArchetypeExtraction, weightShapeStrong, true)
		scores(ArchetypeAgentic, weightShapeAgainst)
	}
	switch {
	case ev.forcedTool:
		// Already counted as a schema above.
	case ev.toolLoop:
		add(ArchetypeAgentic, weightLoop, true)
	case shape.Tools:
		add(ArchetypeAgentic, weightShapeStrong+weightShapeWeak, true)
	case reToolsParam.MatchString(ev.markers):
		// Tools passed by name or held in a config object: the list is
		// not readable from here, but the call still hands the model
		// something to call.
		add(ArchetypeAgentic, weightShapeWeak, true)
	default:
		// A loop with nothing to call is not an agent loop.
		scores(ArchetypeAgentic, weightShapeAgainst)
	}
	if shape.Streaming {
		scores(ArchetypeChat, weightShapeWeak)
	}
	if shape.ImageDetailHigh {
		scores(ArchetypeVision, weightShapeWeak)
	}
	if t := shape.Temperature; t != nil {
		switch {
		case *t == 0:
			for _, arch := range []string{ArchetypeClassification, ArchetypeExtraction,
				ArchetypeModeration, ArchetypeReranking, ArchetypeTranslation} {
				scores(arch, weightShapeWeak)
			}
			scores(ArchetypeChat, weightShapeAgainst)
		case *t >= 0.6:
			scores(ArchetypeChat, weightShapeStrong)
			for _, arch := range []string{ArchetypeClassification, ArchetypeExtraction,
				ArchetypeModeration, ArchetypeReranking} {
				scores(arch, weightShapeAgainst)
			}
		}
	}
	if shape.MaxTokens == nil {
		return
	}
	switch n := *shape.MaxTokens; {
	case n <= capLabel:
		scores(ArchetypeClassification, weightShapeStrong)
		scores(ArchetypeModeration, weightShapeStrong)
		for _, arch := range []string{ArchetypeSummarization, ArchetypeChat, ArchetypeCodegen,
			ArchetypeAgentic, ArchetypeExtraction, ArchetypeTranslation, ArchetypeTranscription,
			ArchetypeVision} {
			scores(arch, weightShapeAgainst)
		}
	case n <= capShort:
		scores(ArchetypeClassification, weightShapeHint)
		scores(ArchetypeModeration, weightShapeHint)
		scores(ArchetypeReranking, weightShapeHint)
	case n >= capLong:
		for _, arch := range []string{ArchetypeSummarization, ArchetypeCodegen, ArchetypeAgentic,
			ArchetypeTranscription, ArchetypeTranslation} {
			scores(arch, weightShapeWeak)
		}
		scores(ArchetypeClassification, weightShapeAgainst)
		scores(ArchetypeModeration, weightShapeAgainst)
		scores(ArchetypeReranking, weightShapeAgainst)
	}
}

// An output schema that is one enum and nothing else asks for a label.
// schemaFacts reads this off a properties block; a bare enum schema,
// which is how the Google SDK spells it, has no properties block to
// read.
func enumOnlyOutput(markers string) bool {
	if !strings.Contains(markers, "enum") || strings.Contains(markers, "properties") {
		return false
	}
	return strings.Contains(markers, "schema") || strings.Contains(markers, "x.enum")
}

// A prompt that says which single category each item goes to is sorting
// in the everyday sense, not the ranking sense, and it is not a digest of
// the items either. Reranking's says stem "sort the" and summarization's
// "digest" both fired on a strict grouping prompt and outvoted the
// category words.
var assignsToOneCategory = []string{
	"into the following categories", "into specific categories", "belong to one group",
	"only belong to one", "use only the categories", "do not create new categories",
}

// A prompt whose answer is a verdict, a boolean or a pass or fail, is
// asking for a decision. It is classification whatever else the prompt
// mentions: a quality gate said its input was "text extracted from a
// PDF" and asked for a "one-sentence summary" as one field, and won for
// extraction at high confidence on those two phrases alone. The verdict
// rules those out the way an image input rules out reading fields.
var reVerdictOutput = regexp.MustCompile(
	`boolean \(true if|boolean, true if|pass or fail|"is_[a-z_]+": boolean`)

var verdictDenies = map[string]bool{
	ArchetypeExtraction:    true,
	ArchetypeSummarization: true,
}

func verdictOutput(prompt string) bool {
	return reVerdictOutput.MatchString(prompt)
}

// A schema whose only field is a boolean is a gate: the call asks for a
// verdict, not for a label or a set of fields.
var reBoolField = regexp.MustCompile(`z\.boolean\(|["']boolean["']`)

func singleBooleanSchema(markers string) bool {
	if len(reBoolField.FindAllString(markers, -1)) != 1 {
		return false
	}
	return strings.Count(markers, "z.") <= 2 && strings.Count(markers, `"type"`) <= 2
}

// rank returns the highest scoring archetype and a confidence. A win
// needs both an absolute score and a margin over the runner up; ties
// fall to archetypePriority, narrowest first. Nothing wins by default:
// too little evidence reports unknown.
func rank(scores map[string]int) (string, string) {
	best, bestScore, secondScore := "", 0, 0
	for _, arch := range archetypePriority {
		s := scores[arch]
		if s > bestScore {
			best, bestScore, secondScore = arch, s, bestScore
		} else if s > secondScore {
			secondScore = s
		}
	}
	if bestScore < scoreFloor {
		return ArchetypeUnknown, "low"
	}
	margin := bestScore - secondScore
	switch {
	case bestScore >= scoreHigh && margin >= marginHigh:
		return best, "high"
	case bestScore >= scoreMedium && margin >= marginMedium:
		return best, "medium"
	default:
		return best, "low"
	}
}

// promptIdents lists identifiers the region assigns as its system
// prompt, in any of the recognized forms.
func promptIdents(region string) []string {
	var names []string
	for _, re := range []*regexp.Regexp{reSystemIdent, reRoleSystem, reTextField} {
		for _, m := range re.FindAllStringSubmatch(region, -1) {
			if v := m[1]; len(v) > 1 {
				names = append(names, v)
			}
		}
	}
	return names
}

// linesAbove returns the offset n line starts above from, bounded by
// lookbackMaxBytes for the same reason headExpand is.
func linesAbove(content string, from, n int) int {
	limit := max(0, from-lookbackMaxBytes)
	i := from
	for k := 0; k <= n; k++ {
		nl := strings.LastIndexByte(content[limit:i], '\n')
		if nl < 0 {
			return limit
		}
		i = limit + nl
	}
	return i + 1
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// Words that flip the phrase after them. "never reply to the customer"
// must not score as "reply to the customer". The Russian and Chinese
// forms sit before the verb the way the English ones do; Korean negates
// after the verb, which the lookback cannot see, so only its "never" is
// here.
var negators = []string{"never ", "not ", "n't ", "avoid ", "without ",
	"не ", "нельзя ", "不要", "请勿", "不得", "禁止", "无需", "切勿", "절대 "}

// Sentence breaks the negation scan stops at, in ASCII and in the full
// width forms Chinese and Korean prose use.
const sentenceBreaks = ".;:\n!?。；：！？"

// saysAny is containsAny for task phrases: a match preceded by a negator
// does not count. Only phrases the prompt asserts score.
func saysAny(s string, phrases []string) bool {
	for _, p := range phrases {
		for from := 0; ; {
			i := strings.Index(s[from:], p)
			if i < 0 {
				break
			}
			at := from + i
			from = at + len(p)
			if !negatedAt(s, at) {
				return true
			}
		}
	}
	return false
}

// negatedAt reports whether a negator sits in the short run of words
// before pos. The scan stops at a sentence break, so a negation in the
// previous sentence does not reach across into this one.
func negatedAt(s string, pos int) bool {
	const lookback = 24
	start := max(0, pos-lookback)
	clause := s[start:pos]
	if cut := strings.LastIndexAny(clause, sentenceBreaks); cut >= 0 {
		// The break may be a multi byte rune; step over the whole of it.
		_, size := utf8.DecodeRuneInString(clause[cut:])
		clause = clause[cut+size:]
	}
	return containsAny(clause, negators)
}
