import boto3

bedrock = boto3.client("bedrock-runtime", region_name="us-east-1")

INSTRUCTION = "Sum up the case history for the next agent in one paragraph. Say what was tried and what is still open."


def case_summary(history: str) -> str:
    response = bedrock.converse(
        modelId="nova-lite",
        messages=[{"role": "user", "content": [{"text": history}]}],
        system=[{"text": INSTRUCTION}],
        inferenceConfig={"maxTokens": 500},
    )
    return response["output"]["message"]["content"][0]["text"]
