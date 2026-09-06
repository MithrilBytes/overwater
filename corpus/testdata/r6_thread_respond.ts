// From usecloudy/cloudy, apps/web/app/api/ai/thread-respond/route.ts, with
// the heliconeAnthropic provider it imports from app/api/utils/helicone.ts,
// which is where the repository keeps it. The model id is the repository's
// own; two commented-out alternatives beside it are dropped so one model
// string is left. The Supabase reads that assemble the thread, filesText and
// contextText are stubbed; the prompt builders are verbatim.

import { createAnthropic } from "@ai-sdk/anthropic";
import { CoreMessage, streamText } from "ai";
import { randomUUID } from "crypto";
import { NextRequest } from "next/server";

export const heliconeAnthropic = createAnthropic({
	baseURL: "https://anthropic.helicone.ai/v1",
	headers: {
		"Helicone-Auth": `Bearer ${process.env.HELICONE_API_KEY}`,
		"Helicone-Property-Env": process.env.NODE_ENV,
		"Helicone-Posthog-Key": process.env.NEXT_PUBLIC_POSTHOG_KEY!,
		"Helicone-Posthog-Host": "https://us.posthog.com",
	},
});

interface ChatMessageRecord {
	id: string;
	role: string;
	content: string;
	selection_text: string | null;
	created_at: string;
}

interface ThreadRespondPostRequestBody {
	threadId: string;
	messageId: string;
}

export const POST = async (req: NextRequest) => {
	const payload = (await req.json()) as ThreadRespondPostRequestBody;

	return respond(payload);
};

const makeSelectionRespondPrompts = ({
	filesText,
	contextText,
	documentContent,
	hasSelection,
	documentTitle,
}: {
	filesText?: string;
	contextText?: string;
	documentContent?: string | null;
	hasSelection?: boolean;
	documentTitle?: string | null;
}): CoreMessage[] => [
	{
		role: "system",
		content: `You are Cloudy, an amazing ideation tool that helps users write technical documents.

${
	filesText
		? `Below are the contents of the files referenced in the document, use this as context:
${filesText}`
		: ""
}

${contextText}
The user is in the process of writing the below document${
			hasSelection
				? `  and has also selected a specific part of the text to talk to you about, the selection is marked with the [[[ and ]]] tags, for example, in the following text: "Hi [[[user]]] I'm doing well", the selection is "user".`
				: ""
		}
You are able to suggest edits to the document as needed, to suggest an edit, you MUST wrap your suggestion in a <suggestion></suggestion> tag, and provide the original content and your new suggestion, for example:

# EXAMPLES:

## Example 1:
<suggestion>
<original_content>
- Wow this is a great idea!
- We can expand it further
</original_content>
<replacement_content>
Wow this is not a bad idea. We can expand it further.
</replacement_content>
</suggestion>

## Example 2:
<suggestion>
<original_content>
- Wow this is a great idea!
- We can expand it further
</original_content>
<replacement_content></replacement_content> # Deletes the lines
</suggestion>

If the user asks you to make a change, you MUST wrap the original content and your new suggestion in a <suggestion></suggestion> tag.
If the user asks you to write something, you MUST wrap your suggestion in a <suggestion></suggestion> tag, following the same format as above.
When making large edits, prefer to use multiple <suggestion></suggestion> tags, rather than one large suggestion.

Below is the document the user is writing${hasSelection ? `, and the selection they have made:` : ":"}
<document${documentTitle ? ` title="${documentTitle}"` : ""}>
${documentContent}
</document>`,
		experimental_providerMetadata: {
			anthropic: { cacheControl: { type: "ephemeral" } },
		},
	},
];

const makeMessageWithSelection = (message: ChatMessageRecord): CoreMessage => {
	return {
		role: message.role as "user" | "assistant",
		content:
			(message.selection_text ? `\`\`\`user selected the text:"${message.selection_text}"\`\`\`\n\n` : "") +
			message.content,
	};
};

const respond = async (payload: ThreadRespondPostRequestBody) => {
	const heliconeHeaders = makeHeliconeHeaders({
		sessionName: "Respond to Thread",
		sessionId: `respond-to-thread/${randomUUID()}`,
	});

	const chatThread = await loadChatThread(payload.threadId);
	const document = chatThread.document;

	const threadMessages = chatThread.messages
		.filter(m => m.id !== payload.messageId)
		.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());

	const hasSelection = false;

	if (!document) {
		throw new Error("Document not found");
	}

	const filesText = await getFileContentsPrompt(document.id);
	let contextText = await getContextForThought(document.id, chatThread.workspace_id, {
		...heliconeHeaders,
		"Helicone-Session-Path": "respond-to-selection/context",
	});

	const llmMessages = makeSelectionRespondPrompts({
		filesText,
		contextText,
		documentContent: document?.content_md,
		hasSelection,
		documentTitle: document?.title,
	});

	threadMessages.forEach(message => {
		llmMessages.push(makeMessageWithSelection(message));
	});

	if (threadMessages.at(-1)?.role !== "user") {
		throw new Error("Last message is not a user message");
	}

	const stream = await streamText({
		model: heliconeAnthropic.languageModel("claude-3-5-sonnet-20241022", { cacheControl: true }),
		messages: llmMessages,
		temperature: 0.0,
		experimental_telemetry: {
			isEnabled: true,
		},
		headers: {
			...heliconeHeaders,
			"Helicone-Session-Path": "thread-respond",
		},
	});

	return stream.toTextStreamResponse();
};

declare function makeHeliconeHeaders(request: { sessionName?: string; sessionId?: string }): Record<string, string>;
declare function loadChatThread(threadId: string): Promise<{
	workspace_id: string;
	document: { id: string; title: string | null; content_md: string | null } | null;
	messages: ChatMessageRecord[];
}>;
declare function getFileContentsPrompt(documentId: string): Promise<string>;
declare function getContextForThought(documentId: string, workspaceId: string, headers: Record<string, string>): Promise<string>;
