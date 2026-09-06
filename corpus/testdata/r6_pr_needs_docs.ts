// From usecloudy/cloudy, apps/web/app/api/ai/create-draft-for-pr/index.ts,
// with the heliconeOpenAI provider it imports from app/api/utils/helicone.ts,
// which is where the repository keeps it. The model id is the repository's
// own. The second call in the file, the Claude document generation that runs
// when the answer is yes, is dropped so one model string is left.

import { createOpenAI } from "@ai-sdk/openai";
import { generateObject } from "ai";
import { z } from "zod";

export const heliconeOpenAI = createOpenAI({
	baseURL: "https://oai.helicone.ai/v1",
	headers: {
		"Helicone-Auth": `Bearer ${process.env.HELICONE_API_KEY}`,
		"Helicone-Property-Env": process.env.NODE_ENV,
		"Helicone-Posthog-Key": process.env.NEXT_PUBLIC_POSTHOG_KEY!,
		"Helicone-Posthog-Host": "https://us.posthog.com",
	},
	compatibility: "strict",
});

export interface PullRequestDocsGenerationDetails {
	title: string;
	description: string;
	diffText: string;
}

interface RepositoryConnectionRecord {
	installation_id: number;
	owner: string;
	name: string;
	project_id: string;
}

const makePrDocsDecisionPrompt = (payload: PullRequestDocsGenerationDetails) => {
	return `Given the following pull request, determine whether the pull request needs any docs.

<pull_request>
<title>
${payload.title}
</title>
<description>
${payload.description}
</description>
<diff>
${payload.diffText}
</diff>
</pull_request>`;
};

export const createDraftForPr = async (
	repositoryConnection: RepositoryConnectionRecord,
	pullRequestNumber: number,
	title: string,
	description: string | null,
	headRef: string,
	baseRef: string,
) => {
	const octokit = getOctokitAppClient(repositoryConnection.installation_id);

	// Get the diff between base and head
	const comparison = await octokit.rest.repos.compareCommitsWithBasehead({
		owner: repositoryConnection.owner,
		repo: repositoryConnection.name,
		basehead: `${baseRef}...${headRef}`,
	});

	const diffText = comparison.data.files?.map((file: { patch?: string }) => file.patch).join("\n\n") ?? "";

	const {
		object: { needsDocs },
	} = await generateObject({
		model: heliconeOpenAI.languageModel("gpt-4o-mini-2024-07-18"),
		prompt: makePrDocsDecisionPrompt({ title, description: description ?? "", diffText }),
		schema: z.object({
			needsDocs: z.boolean().describe("Whether the pull request needs any docs"),
		}),
	});

	if (!needsDocs) {
		// Create a comment saying we don't need docs
		await octokit.rest.issues.createComment({
			owner: repositoryConnection.owner,
			repo: repositoryConnection.name,
			issue_number: pullRequestNumber,
			body: `👋 Looks like your changes don't need any docs, you're all clear!`,
		});

		return;
	}

	// 1. Determine whether the pr needs any docs
	// 2. Generate the docs, we'll need to support as many pages as needed
	return needsDocs;
};

declare function getOctokitAppClient(installationId: number): any;
