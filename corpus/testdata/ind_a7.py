import anthropic
c = anthropic.Anthropic()
def localize(md):
    return c.messages.create(model="claude-sonnet-5", max_tokens=2000,
        system="Render the following help article in Spanish. Keep all markdown structure and code blocks exactly as they are.",
        messages=[{"role":"user","content":md}])
