from langchain_openai import OpenAIEmbeddings
from langchain_community.vectorstores import FAISS

embeddings = OpenAIEmbeddings(model="text-embedding-ada-002")


def build_index(docs):
    return FAISS.from_documents(docs, embeddings)
