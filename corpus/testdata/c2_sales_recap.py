import boto3

bedrock = boto3.client("bedrock-runtime", region_name="us-east-1")


def recap_sales_call(transcript: str) -> str:
    response = bedrock.converse(
        modelId="nova-pro",
        system=[{"text": "Condense the sales call transcript into next steps and blockers for the account team."}],
        messages=[{"role": "user", "content": [{"text": transcript}]}],
        inferenceConfig={"maxTokens": 512, "temperature": 0.2},
    )
    return response["output"]["message"]["content"][0]["text"]
