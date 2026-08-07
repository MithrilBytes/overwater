import boto3

bedrock = boto3.client("bedrock-runtime", region_name="us-east-1")


def assess_photo(image_bytes: bytes) -> str:
    response = bedrock.converse(
        modelId="nova-pro",
        messages=[
            {
                "role": "user",
                "content": [
                    {"image": {"format": "jpeg", "source": {"bytes": image_bytes}}},
                    {"text": "Describe the visible damage to the vehicle in this photo and which panels it touches."},
                ],
            }
        ],
        inferenceConfig={"maxTokens": 500},
    )
    return response["output"]["message"]["content"][0]["text"]
