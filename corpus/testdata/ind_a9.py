import anthropic
c = anthropic.Anthropic()
def scaffold(spec):
    return c.messages.create(model="claude-opus-5", max_tokens=4000,
        system="Write a Python function implementing the described behavior. Return only the code.",
        messages=[{"role":"user","content":spec}])
