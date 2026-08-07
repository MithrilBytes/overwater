import boto3

bedrock = boto3.client("bedrock-runtime", region_name="us-west-2")

FIELD_TOOL = {
    "tools": [
        {
            "toolSpec": {
                "name": "record_claim",
                "description": "Record the fields read off an insurance claim form.",
                "inputSchema": {
                    "json": {
                        "type": "object",
                        "properties": {
                            "policy_number": {"type": "string"},
                            "incident_date": {"type": "string"},
                            "amount_claimed": {"type": "number"},
                        },
                    }
                },
            }
        }
    ],
    "toolChoice": {"tool": {"name": "record_claim"}},
}


def read_claim(form_text: str) -> dict:
    response = bedrock.converse(
        modelId="claude-sonnet-4-5",
        messages=[{"role": "user", "content": [{"text": form_text}]}],
        toolConfig=FIELD_TOOL,
        inferenceConfig={"maxTokens": 400, "temperature": 0.0},
    )
    return response["output"]["message"]
