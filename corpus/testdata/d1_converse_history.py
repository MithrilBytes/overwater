import boto3

bedrock = boto3.client("bedrock-runtime", region_name="us-east-1")


def next_reply(history: list[dict]) -> str:
    response = bedrock.converse(
        modelId="nova-pro",
        messages=history,
        system=[{"text": "You are the Northwind travel desk assistant. Be conversational and concise."}],
        inferenceConfig={"maxTokens": 900, "temperature": 0.7},
    )
    return response["output"]["message"]["content"][0]["text"]
