import boto3

bedrock = boto3.client("bedrock-runtime", region_name="us-west-2")

TOOL_CONFIG = {
    "tools": [
        {"toolSpec": {"name": "query_warehouse", "description": "Run a read only SQL query.", "inputSchema": {"json": {"type": "object", "properties": {"sql": {"type": "string"}}}}}},
        {"toolSpec": {"name": "post_chart", "description": "Post a chart to the channel.", "inputSchema": {"json": {"type": "object", "properties": {"url": {"type": "string"}}}}}},
    ]
}


def take_turn(history: list[dict]) -> dict:
    """history carries the tool results from earlier turns."""
    return bedrock.converse(
        modelId="claude-sonnet-5",
        messages=history,
        system=[{"text": "You are the analytics agent. Keep querying until you can answer, then post the chart."}],
        toolConfig=TOOL_CONFIG,
        inferenceConfig={"maxTokens": 3000},
    )
