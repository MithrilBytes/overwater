// From pipijoe/xryder-server,
// src/main/java/cn/xryder/base/ai/agent/MonitorAgent.java, trimmed to the
// chart picking call. The repository sets the model in
// src/main/resources/application.yml, spring.ai.openai.chat.options.model
// GLM-4-flash against the bigmodel.cn base url; that id is not in the
// catalog, so glm-4.5-air is substituted, as default options on the client
// the constructor builds. The prompts are Chinese in the original and are
// kept that way.

package cn.xryder.base.ai.agent;

import org.springframework.ai.chat.client.ChatClient;
import org.springframework.ai.chat.prompt.Prompt;
import org.springframework.ai.chat.prompt.PromptTemplate;
import org.springframework.ai.openai.OpenAiChatModel;
import org.springframework.ai.openai.OpenAiChatOptions;
import org.springframework.stereotype.Component;

import java.util.Map;

/**
 * 监控分析AI智能体
 */
@Component
public class MonitorAgent {
    private final ChatClient chatClient;

    public MonitorAgent(OpenAiChatModel chatModel) {
        String systemPrompt = """
                你是莱德队长，一个数据库专家

                """;
        this.chatClient = ChatClient.builder(chatModel)
                .defaultSystem(systemPrompt)
                .defaultOptions(OpenAiChatOptions.builder().model("glm-4.5-air").build())
                .build();
    }

    public String getChartType(String question, String data) {
        String template = """
                请根据问题和数据推荐一个合适的用于展示该数据的图表, 只需要给出图表类型的英文名称。

                问题如下:

                {question}

                数据如下：

                {data}

                可选择的图表类型如下：

                Area, Line, Bar, Radar

                以下是一个具体示例：
                问题：统计最近30天每天的网站访问量
                你的回答：Line
                """;
        PromptTemplate promptTemplate = new PromptTemplate(template);
        Prompt prompt = promptTemplate.create(Map.of("question", question, "data", data));
        return chatClient.prompt()
                .user(prompt.getContents())
                .call().content();
    }
}
