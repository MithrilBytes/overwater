// From jinia91/blog,
// service/ai-second-brain/ai-second-brain-core/src/main/kotlin/kr/co/jiniaslog/ai/domain/agent/MemoManagementAgent.kt,
// with the memoChatClient bean from service/AiConfig.kt and two of the ten
// tools from domain/agent/MemoTools.kt in the same module. The Gemini model
// id is configured for Spring AI's google-genai starter outside the tree, so
// the catalog id gemini-2.5-flash is set on the bean here. The parser that
// turns tool result strings into typed responses is dropped and the text is
// returned as is; the prompt is Korean in the original and is kept that way.

package kr.co.jiniaslog.ai.domain.agent

import kr.co.jiniaslog.ai.outbound.MemoCommandService
import org.slf4j.LoggerFactory
import org.springframework.ai.chat.client.ChatClient
import org.springframework.ai.chat.client.advisor.MessageChatMemoryAdvisor
import org.springframework.ai.chat.memory.ChatMemory
import org.springframework.ai.chat.model.ChatModel
import org.springframework.ai.google.genai.GoogleGenAiChatOptions
import org.springframework.ai.tool.annotation.Tool
import org.springframework.ai.tool.annotation.ToolParam
import org.springframework.beans.factory.annotation.Qualifier
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.stereotype.Component
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter

@Configuration
class AiConfig {
    /**
     * 4. Memo Agent용 ChatClient (Tool Calling 지원)
     * Tools는 런타임에 주입됨
     */
    @Bean("memoChatClient")
    fun memoChatClient(
        @Qualifier("googleGenAiChatModel") chatModel: ChatModel
    ): ChatClient {
        return ChatClient.builder(chatModel)
            .defaultOptions(GoogleGenAiChatOptions.builder().model("gemini-2.5-flash").build())
            .build()
    }
}

/**
 * Memo/Folder Agent에서 사용하는 Tool 정의
 * Spring AI의 Tool Calling 기능을 사용하여 메모와 폴더를 관리합니다.
 */
@Component
class MemoTools(
    private val memoCommandService: MemoCommandService
) {
    private var currentAuthorId: Long = 0

    fun setAuthorId(authorId: Long) {
        this.currentAuthorId = authorId
    }

    @Tool(description = "현재 날짜와 시간을 조회합니다. 상대적 시간 표현(내일, 모레, 다음주 등)을 실제 날짜로 변환할 때 사용하세요.")
    fun getCurrentDateTime(): String {
        val now = LocalDateTime.now()
        val formatter = DateTimeFormatter.ofPattern("yyyy년 MM월 dd일 (E) HH:mm")
        val dateStr = now.format(formatter)

        return """현재 시간: $dateStr
오늘: ${now.format(DateTimeFormatter.ofPattern("MM월 dd일 (E)"))}
내일: ${now.plusDays(1).format(DateTimeFormatter.ofPattern("MM월 dd일 (E)"))}
모레: ${now.plusDays(2).format(DateTimeFormatter.ofPattern("MM월 dd일 (E)"))}"""
    }

    @Tool(description = "사용자의 메모를 생성합니다. 적절한 제목과 내용을 추출하여 저장합니다.")
    fun createMemo(
        @ToolParam(description = "메모 제목 (간결하고 명확하게, 최대 50자)") title: String,
        @ToolParam(description = "메모 내용") content: String
    ): String {
        val memoId = memoCommandService.createMemo(
            authorId = currentAuthorId,
            title = title,
            content = content
        )
        return "MEMO_CREATED:$memoId:$title:메모가 생성되었습니다."
    }
}

/**
 * Memo Management Agent - 메모와 폴더를 관리하는 AI 에이전트
 *
 * Tool Calling을 통해 다음 작업을 수행합니다:
 * - 메모: 생성, 수정, 삭제, 폴더 이동, 목록 조회
 * - 폴더: 생성, 이름 변경, 삭제, 상위 폴더 이동, 목록 조회
 *
 * 삭제 작업은 프롬프트에서 사용자 확인을 받도록 지시합니다.
 * ChatMemory를 사용하여 대화 히스토리를 유지하고, 삭제 확인 컨텍스트를 이해합니다.
 */
@Component
class MemoManagementAgent(
    @Qualifier("memoChatClient") private val chatClient: ChatClient,
    private val memoTools: MemoTools,
    private val chatMemory: ChatMemory
) {
    private val log = LoggerFactory.getLogger(javaClass)

    companion object {
        private const val SYSTEM_PROMPT = """메모/폴더 관리 어시스턴트입니다.

## 절대 규칙
1. "~합니다", "~할게요" 말하지 마세요. 바로 도구를 호출하세요!
2. 사용자에게 ID 묻지 마세요. listMemos/listFolders로 조회하세요.
3. 삭제만 확인, 나머지는 즉시 실행

## 시간 표현 처리 (중요!)
사용자가 상대적 시간 표현을 사용하면 반드시 실제 날짜로 변환하세요:
1. 메모 생성 전에 getCurrentDateTime() 호출
2. 상대적 표현 → 실제 날짜로 변환하여 저장

변환 예시:
- "내일 5시 약속" → "2월 8일(토) 17:00 약속"
- "모레 회의" → "2월 9일(일) 회의"
- "다음주 월요일 출장" → "2월 10일(월) 출장"
- "이번주 토요일 저녁" → "2월 8일(토) 저녁"

주의: "내일", "모레", "다음주" 같은 단어를 그대로 저장하면 나중에 의미가 없어집니다!

## 메모 생성 시 자동 폴더 배정
메모를 생성할 때 반드시 다음 순서로 처리하세요:
1. getCurrentDateTime() 호출 (시간 표현 있을 경우)
2. listFolders() 호출하여 기존 폴더 목록 확인
3. 메모 내용/제목과 관련된 폴더가 있는지 판단:
   - 관련 폴더 있음: createMemo() 후 moveMemoToFolder()로 해당 폴더에 배정
   - 관련 폴더 없고 카테고리가 명확함: createFolder()로 적절한 폴더 생성 후 배정
   - 일반적인 메모/분류 불가: 루트에 저장 (폴더 배정 안함)

폴더 매칭 예시:
- "5시 약속있다" + 폴더 "일정" 있음 → 일정 폴더에 저장
- "우유 사야함" + 폴더 "쇼핑" 있음 → 쇼핑 폴더에 저장
- "프로젝트 회의록" + 폴더 없음 → "업무" 또는 "회의" 폴더 생성 후 저장
- "그냥 메모" + 분류 불가 → 루트에 저장

## 작업 예시

"내일 밤 10시 jvm 스터디":
→ getCurrentDateTime() 호출 (오늘 날짜 확인)
→ listFolders() 호출 (폴더 확인)
→ createMemo(title="2월 8일(토) 22:00 JVM 스터디", content="2월 8일 토요일 밤 10시 JVM 스터디") 호출
→ 관련 폴더 있으면 moveMemoToFolder() 호출
→ "메모가 저장되었습니다" 응답

"메모 폴더로 정리해":
→ listMemos() 호출
→ listFolders() 호출
→ moveMemoToFolder(memoId, folderId) 호출
→ "메모 'X'를 'Y' 폴더로 이동했습니다" 응답

## 도구
- getCurrentDateTime: 현재 시간 조회 (상대적 시간 변환용)
- listMemos, listFolders: 조회
- createMemo, updateMemo, moveMemoToFolder
- createFolder, renameFolder, moveFolderToParent
- deleteMemo, deleteFolder → 확인 필수

## 금지
- "이동합니다", "이동할게요" 등 예고 금지
- 도구 호출 없이 응답 금지
- ID 질문 금지
- "내일", "모레" 등 상대적 시간을 그대로 저장 금지"""
    }

    /**
     * 사용자 메시지를 처리하고 적절한 작업을 수행합니다.
     * sessionId를 conversationId로 사용하여 다른 에이전트와 메모리를 공유합니다.
     */
    fun process(message: String, authorId: Long, sessionId: Long): String {
        memoTools.setAuthorId(authorId)

        // RagAgent와 동일한 conversationId 사용하여 컨텍스트 공유
        val conversationId = sessionId.toString()

        val response = try {
            chatClient.prompt()
                .system(SYSTEM_PROMPT)
                .user(message)
                .tools(memoTools)
                .advisors(
                    MessageChatMemoryAdvisor.builder(chatMemory)
                        .conversationId(conversationId)
                        .build()
                )
                .call()
                .content() ?: "요청을 처리할 수 없습니다."
        } catch (e: Exception) {
            log.error("MemoManagementAgent error: ${e.message}", e)
            return "처리 중 오류가 발생했습니다: ${e.message}"
        }

        log.debug("MemoManagementAgent response: $response")
        return response
    }
}
