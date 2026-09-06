"""From haandol/claudecrawl, src/crawler.py, with SYSTEM_PROMPT and USER_PROMPT
from src/prompt.py, the ChatBedrockConverse construction from src/llm.py, and
MODEL_ID with its env default plus the article schema and entry point from
app.py. The repository's default, us.anthropic.claude-3-5-haiku-20241022-v1:0,
is not in the catalog, so the Bedrock id of the nearest priced Haiku is
substituted. The Phoenix tracing setup in src/llm.py is dropped."""

import os
from typing import List, Optional, Type

from langchain_aws.chat_models import ChatBedrockConverse
from langchain_core.language_models import BaseChatModel
from langchain_core.prompts import ChatPromptTemplate
from pydantic import BaseModel, Field

from src.scraper import PlayWrightScraper, Scraper


MODEL_ID = os.environ.get("MODEL_ID", "us.anthropic.claude-haiku-4-5-20251001-v1:0")
AWS_PROFILE_NAME = os.environ.get("AWS_PROFILE_NAME", None)
AWS_REGION = os.environ.get("AWS_REGION", None)


SYSTEM_PROMPT = """
You are tasked with scraping information from a web page and extracting specific details based on a given output schema. 
Your task is to carefully read and analyze the content of this web page, and then extract information according to the provided output schema.
The web page will be provided within the tripple backticks.

## Instruction

1. To begin, thoroughly read and analyze the entire web page. \
Pay attention to all sections, including headers, paragraphs, lists, tables, and any other relevant elements. \
Take note of the overall structure and organization of the content.
2. As you analyze the page, identify information that matches the fields specified in the output schema. \
Be thorough and precise in your extraction.

## HTML Analysis
- Examine the HTML code and identify elements, classes, or IDs that correspond to each required data field
- Look for patterns or repeated structures that could indicate multiple items (e.g., product listings).
- Note any nested structures or relationships between elements that are relevant to the data extraction task.
- Discuss any additional considerations based on the specific HTML layout that are crucial for accurate data extraction.
- Recommend the specific strategy to use for scraping the content, remeber.

## Data Analysis
- List out all the links in the page, to make a group by their similarity.
- Meaningful data has a tendency to be around a link url, such as `a` tag.
- Article links tends to have similar link url, `href` prop, which out numbers the most of the links in the page.

## Link Extraction
- Do not create any of links, if the content has no link for the schema. \
In that case, just respond with empty string

Begin your scraping process now, and provide the extracted information in the format specified above. Let's think step by step.
""".strip()

USER_PROMPT = """
Here is the web page content.
```html
{html_content}
```

{instruction}
""".strip()


class Article(BaseModel):
    """
    LOL(League of Legends) champion tatic article schema.
    Contains details about how to play and win with a specific LOL champion.
    """

    title: str = Field(..., description="The title of the article.")
    url: str = Field(..., description="The URL link to the article.")
    season: int = Field(..., description="The season of the article.")
    published_at: str = Field(..., description="The published date of the article in RFC 3339 format.")


class OutputSchema(BaseModel):
    """
    Schema to extract articles for the LOL tatic from the page.
    """

    articles: List[Article] = Field(
        [], description="A list of LOL champion tactic article objects extracted from the page."
    )


class BedrockLLM(object):
    def __init__(
        self,
        model: str,
        aws_profile_name: Optional[str] = None,
        aws_region: Optional[str] = None,
        temperature: float = 0.2,
        max_tokens: int = 1024 * 4,
    ):
        self.model = ChatBedrockConverse(
            model=model,
            credentials_profile_name=aws_profile_name,
            region_name=aws_region,
            temperature=temperature,
            max_tokens=max_tokens,
        )


class ClaudeCrawler:
    def __init__(self, scraper: Scraper, model: BaseChatModel, output_schema: Type[BaseModel]):
        self.scraper = scraper
        self.model = model.with_structured_output(output_schema)
        self.system_prompt = SYSTEM_PROMPT
        self.user_prompt = USER_PROMPT

    def extract(self, instruction: str, html_content: str):
        prompt_value = ChatPromptTemplate(
            [
                ("system", SYSTEM_PROMPT),
                ("human", USER_PROMPT),
            ]
        ).invoke({"html_content": html_content, "instruction": instruction})
        return self.model.invoke(prompt_value)

    def crawl(self, url: str, instruction: str):
        html_content = self.scraper.scrape(url)
        return self.extract(instruction, html_content)


def main(url: str, instruction: str, output_schema: Type[BaseModel]):
    """
    Main function to crawl and extract LOL champion tactic articles from a webpage.
    """
    llm = BedrockLLM(
        model=MODEL_ID,
        aws_profile_name=AWS_PROFILE_NAME,
        aws_region=AWS_REGION,
    )
    scraper = PlayWrightScraper()
    crawler = ClaudeCrawler(scraper, llm.model, output_schema)
    return crawler.crawl(url, instruction)


if __name__ == "__main__":
    url = "https://lol.inven.co.kr/dataninfo/champion/manualTool.php?confirm=2&season=14"

    instruction = (
        "Please extract the LOL champion tactic articles from the page."
        "The articles are listed at the <table> tag in the page."
    )
    resp = main(url, instruction, OutputSchema)
