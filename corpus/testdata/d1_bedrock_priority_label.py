import boto3

bedrock = boto3.client("bedrock-runtime", region_name="us-east-1")

RUBRIC = "Assign the incident a priority: P1, P2, or P3. Answer with the code only."


def priority_of(incident: str) -> str:
    response = bedrock.converse(
        modelId="nova-micro",
        messages=[{"role": "user", "content": [{"text": incident}]}],
        system=[{"text": RUBRIC}],
        inferenceConfig={"maxTokens": 5, "temperature": 0.0},
    )
    return response["output"]["message"]["content"][0]["text"].strip()
