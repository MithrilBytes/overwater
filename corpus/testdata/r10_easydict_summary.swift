// From tisfeng/Easydict, Easydict/Swift/Service/AITool/SummaryService.swift, with
// the built in service it runs on from Service/AITool/AIToolService.swift and
// Service/BuiltInAI/BuiltInAIService.swift, and the request from
// Service/OpenAI/BaseOpenAIService.swift, which is where the repository keeps
// them. The built in default model, ZhipuModel.glm_4_flash_250414, is not in the
// catalog; the Groq entry the same defaultModels list carries is used as the
// default here and the Zhipu cases are dropped so one model string is left. The
// endpoint checks and the streaming branch of the request are trimmed.

import Defaults
import Foundation
import OpenAI

func chatMessagePair(userContent: String, assistantContent: String) -> [ChatMessage] {
    [
        .init(role: .user, content: userContent),
        .init(role: .assistant, content: assistantContent),
    ]
}

// MARK: - BaseOpenAIService

@objcMembers
@objc(EZBaseOpenAIService)
public class BaseOpenAIService: StreamService {
    typealias OpenAIChatMessage = ChatQuery.ChatCompletionMessageParam

    override func contentStreamTranslate(
        _ text: String,
        from: Language,
        to: Language
    )
        -> AsyncThrowingStream<String, any Error> {
        let url = URL(string: endpoint)!

        result.isStreamFinished = false

        let queryType = queryType(text: text, from: from, to: to)
        let chatQueryParam = ChatQueryParam(
            text: text,
            sourceLanguage: from,
            targetLanguage: to,
            queryType: queryType,
            enableSystemPrompt: true
        )

        let chatHistory = serviceChatMessageModels(chatQueryParam)
        guard let chatHistory = chatHistory as? [OpenAIChatMessage] else {
            let error = QueryError(
                type: .parameter, message: "Failed to convert chat messages"
            )
            return AsyncThrowingStream { continuation in
                continuation.finish(throwing: error)
            }
        }

        let query = ChatQuery(messages: chatHistory, model: model, temperature: temperature)

        return nonStreamingTranslate(query: query, url: url)
    }

    override func serviceChatMessageModels(_ chatQuery: ChatQueryParam) -> [Any] {
        var chatMessages: [OpenAIChatMessage] = []
        for message in chatMessageDicts(chatQuery) {
            let openAIRole = message.role.rawValue
            let content = message.content

            if let role = OpenAIChatMessage.Role(rawValue: openAIRole),
               let chat = OpenAIChatMessage(role: role, content: content) {
                chatMessages.append(chat)
            }
        }
        return chatMessages
    }
}

// MARK: - BuiltInAIService

@objc(EZBuiltInAIService)
class BuiltInAIService: BaseOpenAIService {
    // MARK: Lifecycle

    required init() {
        super.init()

        // Set default supported models, disable user to change it.
        // Generally, it should be updated only when the app is updated.
        supportedModels = defaultModels.joined(separator: ", ")
    }

    // MARK: Public

    public override func name() -> String {
        NSLocalizedString("built_in_ai", comment: "")
    }

    public override func serviceType() -> ServiceType {
        .builtInAI
    }

    public override func apiKeyRequirement() -> ServiceAPIKeyRequirement {
        .builtIn
    }

    // MARK: Internal

    override var defaultModels: [String] {
        [
            // Groq free models
            GroqModel.llama3_1_8b_instant.rawValue,
        ]
    }

    override var defaultModel: String {
        GroqModel.llama3_1_8b_instant.rawValue
    }

    override var canFetchRemoteModels: Bool {
        false
    }

    override var apiKey: String {
        builtInAIAPIKey
    }

    override var endpoint: String {
        builtInAIEndpoint
    }

    override var observeKeys: [Defaults.Key<String>] {
        [supportedModelsKey]
    }
}

// MARK: - GroqModel

enum GroqModel: String, CaseIterable {
    // Docs: https://console.groq.com/docs/models
    // Limits: https://console.groq.com/settings/limits

    case llama3_1_8b_instant = "llama-3.1-8b-instant" // 30 RPM, 14.4k RPD, 6k TPM, 500k TPD
}

// MARK: - AIToolService

/// A class used for AI Tools such as summary and polishing
class AIToolService: BuiltInAIService {
    // MARK: Public

    public override func configurationListItems() -> Any {
        StreamConfigurationView(
            service: self,
            showCustomNameSection: false,
            showAPIKeySection: false,
            showEndpointSection: false,
            showSupportedModelsSection: false,
            showUsedModelSection: true,
            showCustomPromptSection: false,
            showTranslationToggle: false,
            showSentenceToggle: false,
            showDictionaryToggle: false,
            showUsageStatusPicker: true
        )
    }

    public override func apiKeyRequirement() -> ServiceAPIKeyRequirement {
        .builtIn
    }

    // MARK: Internal

    override var serviceUsageStatusKey: Defaults.Key<ServiceUsageStatus> {
        serviceDefaultsKey(.serviceUsageStatus, defaultValue: .alwaysOff)
    }
}

// MARK: - SummaryService

// swiftlint:disable line_length

@objc(EZSummaryService)
class SummaryService: AIToolService {
    // MARK: Public

    public override func name() -> String {
        NSLocalizedString("summary_service", comment: "")
    }

    public override func serviceType() -> ServiceType {
        .summary
    }

    // MARK: Internal

    override func chatMessageDicts(_ chatQuery: ChatQueryParam) -> [ChatMessage] {
        let (text, sourceLanguage, _, _, _) = chatQuery.unpack()
        let answerLanguage = MyConfiguration.shared.firstLanguage
        let prompt = summaryPrompt(
            text: text, sourceLanguage: sourceLanguage, answerLanguage: answerLanguage
        )

        let fewShot = [
            chatMessagePair(
                userContent:
                "Using English to summarize the following English text: \"\"\"The quick brown fox jumps over the lazy dog. The fox is very quick and agile, making it difficult for the dog to catch up. Despite several attempts, the dog remains lazy and doesn't put in much effort to chase the fox.\"\"\".",
                assistantContent:
                "The quick and agile fox jumps over the lazy dog, who remains lazy and doesn't put in much effort to chase the fox."
            ),
            chatMessagePair(
                userContent:
                "Using Simplified-Chinese to summarize the following text: \"\"\"联合国在西非地区的最高官员周五表示，马里、布基纳法索和尼日尔决定退出西非国家经济共同体，将全面破坏地区关系，而此时恐怖主义和跨国有组织犯罪仍然对该地区构成普遍威胁。联合国西非和萨赫勒办事处负责人莱昂纳多•桑托斯•西芒向安理会表示，放弃西非经共体 将使三个军方领导的政府放弃关键利益，包括地区一体化、行动自由、安全合作和一体化的地区经济，这既伤害了他们自己，也伤害了西非经共体的其他成员。在高级军官分别于 2021 年、2022 年和 2023 年发动军事接管后，这三个过渡政府断绝了与西非经共体的关系。西芒说，军事领导人因此推迟了恢复宪政的时间，并引发了对长期不确定性的恐惧，因为公民空间继续缩小。\"\"\".",
                assistantContent:
                "马里、布基纳法索和尼日尔退出西非国家经济共同体，将严重破坏地区关系，尤其是在恐怖主义和跨国有组织犯罪仍威胁该地区的情况下。联合国官员西芒指出，这一决定将使这三个国家失去地区一体化、安全合作和经济利益，推迟恢复宪政，并加剧长期不确定性。"
            ),

        ].flatMap { $0 }

        var messages: [ChatMessage] = [
            .init(role: .system, content: summarySystemPrompt),
        ]
        messages.append(contentsOf: fewShot)
        messages.append(.init(role: .user, content: prompt))

        return messages
    }

    // MARK: Private

    private let summarySystemPrompt = """
    You are a text summarization expert proficient in condensing lengthy documents, articles, and other text formats into concise and coherent summaries. Your summaries should capture the main points, key details, and overall essence of the original text while maintaining clarity and accuracy. Avoid adding personal opinions or interpretations. Only return the summary, without including redundant quotes or additional notes.
    """

    private func summaryPrompt(
        text: String,
        sourceLanguage: Language,
        answerLanguage: Language
    )
        -> String {
        "Using \(answerLanguage.rawValue) to summarize the following \(sourceLanguage.queryLanguageName) text: \"\"\"\(text)\"\"\"."
    }
}

// swiftlint:enable line_length
