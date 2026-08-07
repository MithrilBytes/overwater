from openai import OpenAI
c = OpenAI()
def process_data(blob):
    return c.chat.completions.create(model="gpt-5.1", temperature=0, max_tokens=300,
        response_format={"type":"json_schema","json_schema":{"name":"inv","schema":{"type":"object","properties":{"invoice_no":{"type":"string"},"issued_on":{"type":"string"},"total_cents":{"type":"integer"}}}}},
        messages=[{"role":"user","content":"Pull the invoice number, issue date, and total from this document.\n\n"+blob}])
