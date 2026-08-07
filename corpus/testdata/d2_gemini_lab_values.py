from google import genai
from google.genai import types

client = genai.Client()

REPORT_SCHEMA = {
    "type": "OBJECT",
    "properties": {
        "patient_id": {"type": "STRING"},
        "collected_on": {"type": "STRING"},
        "hemoglobin": {"type": "NUMBER"},
        "white_cell_count": {"type": "NUMBER"},
    },
}


def read_lab_report(report: str) -> str:
    result = client.models.generate_content(
        model="gemini-2.5-flash",
        contents="Copy the listed values out of this lab report:\n\n" + report,
        config=types.GenerateContentConfig(
            response_mime_type="application/json",
            response_schema=REPORT_SCHEMA,
            temperature=0,
            max_output_tokens=400,
        ),
    )
    return result.text
