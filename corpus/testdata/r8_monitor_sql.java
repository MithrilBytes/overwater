// From pipijoe/xryder-server,
// src/main/java/cn/xryder/base/ai/agent/MonitorAgent.java, trimmed to the
// SQL generating call. The repository sets the model in
// src/main/resources/application.yml, spring.ai.openai.chat.options.model
// GLM-4-flash against the bigmodel.cn base url; that id is not in the
// catalog, so glm-4.5-air is substituted, on the per call options the method
// already builds. The doubled dash inside the visit date column name in the
// table notes is flattened to an underscore. The prompts are Chinese in the
// original and are kept that way.

package cn.xryder.base.ai.agent;

import org.springframework.ai.chat.client.ChatClient;
import org.springframework.ai.chat.messages.Message;
import org.springframework.ai.chat.messages.SystemMessage;
import org.springframework.ai.chat.prompt.Prompt;
import org.springframework.ai.chat.prompt.PromptTemplate;
import org.springframework.ai.openai.OpenAiChatModel;
import org.springframework.ai.openai.OpenAiChatOptions;
import org.springframework.stereotype.Component;

import java.util.List;
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
                .build();
    }

    public String getDataSourceMeta() {
        return """
                数据库表元信息：
                表1名称:
                monitor_visitor

                表1描述：
                monitor_visitor 是一个记录用户访问系统记录的表。

                字段信息：
                id
                类型：BIGINT (bigint)
                属性：自增主键
                描述：唯一标识每条系统访问记录。

                useruuid
                类型：VARCHAR
                属性：非空
                描述：用户标识，一般基于用户的浏览器生成唯一标识。

                visit_date
                类型：DATETIME
                属性：非空
                描述：系统访问时间。
                表用途：
                记录独立访客，当天如果用户有访问系统会记录一条内容。

                任务指引：
                如果需要根据上述表生成 SQL 查询，确保字段名和类型的正确性，返回字段是统计值的用value表示，放在返回字段最后。
                """;
    }

    public SystemMessage getSystemMessage() {
        String systemText = """
                你是一个专业的数据库管理员，非常擅长处理SQL。

                SQL 语句应符合以下要求：

                不使用任何 Markdown 或代码块标识（例如 sql）。
                只返回纯 SQL 语句，不附带额外的注释或说明。
                """;
        return new SystemMessage(systemText);
    }

    public Message getUserMessage(String question) {
        String context = getDataSourceMeta();
        String userText = """
                请生成标准的 SQL 查询语句。

                {context}

                问题：
                {question}
                """;
        PromptTemplate promptTemplate = new PromptTemplate(userText);
        return promptTemplate.createMessage(Map.of("context", context, "question", question));
    }

    public String generateSql(String question) {
        SystemMessage systemMessage = getSystemMessage();
        Message userMessage = getUserMessage(question);
        Prompt prompt = new Prompt(List.of(userMessage, systemMessage));
        OpenAiChatOptions options = OpenAiChatOptions.builder().model("glm-4.5-air").temperature(0.7).build();
        return chatClient.prompt(prompt).options(options).call().content();
    }

    // 清理 SQL 的方法
    private String cleanSql(String sql) {
        return sql.replaceAll("```", "").replace("sql", "").trim();
    }

    // 调用大模型重新生成 SQL
    private String generateSqlWithErrorHandling(String originalSql, String errorMessage) {
        String question = "请根据以下 SQL 和错误信息修复 SQL：" + "\nSQL: " + originalSql + "\n错误信息: " + errorMessage + "\n注意：只需要返回新生成的SQL。";
        return generateSql(question);
    }
}
