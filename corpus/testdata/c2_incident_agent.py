import boto3

bedrock = boto3.client("bedrock-runtime", region_name="us-west-2")

TOOL_CONFIG = {
    "tools": [
        {
            "toolSpec": {
                "name": "get_alert",
                "description": "Fetch an open alert by id.",
                "inputSchema": {"json": {"type": "object", "properties": {"alert_id": {"type": "string"}}}},
            }
        },
        {
            "toolSpec": {
                "name": "restart_service",
                "description": "Restart a systemd unit on a host.",
                "inputSchema": {"json": {"type": "object", "properties": {"host": {"type": "string"}, "unit": {"type": "string"}}}},
            }
        },
    ]
}


def run_incident_step(history: list[dict]) -> dict:
    response = bedrock.converse(modelId="nova-lite", messages=history, toolConfig=TOOL_CONFIG)
    return response["output"]["message"]
