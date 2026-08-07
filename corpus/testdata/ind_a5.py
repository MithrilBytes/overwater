from openai import OpenAI
c = OpenAI()
def build_index(chunks):
    return c.embeddings.create(model="text-embedding-3-large", input=chunks)
