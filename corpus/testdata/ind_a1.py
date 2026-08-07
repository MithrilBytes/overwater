import anthropic
c = anthropic.Anthropic()
def handle(ticket):
    return c.messages.create(model="claude-opus-5", max_tokens=900,
        system="You are Nora, our billing support agent. Reply to the customer warmly and answer their question using the account notes below.",
        messages=[{"role":"user","content":ticket}])
