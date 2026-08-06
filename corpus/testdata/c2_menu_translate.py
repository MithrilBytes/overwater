import cohere

co = cohere.ClientV2()


def translate_menu_item(item_text: str, target_lang: str) -> str:
    resp = co.chat(
        model="command-r-08-2024",
        messages=[
            {"role": "system", "content": "Translate restaurant menu copy. Keep dish names, translate the descriptions."},
            {"role": "user", "content": f"Target language: {target_lang}\n\n{item_text}"},
        ],
    )
    return resp.message.content[0].text
