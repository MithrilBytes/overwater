package evalgen

// Plain Python, no framework: readable before running, editable after.
// Every chat template shares one scoring body; the provider half
// supplies the header, imports, client, and the single ask call.

// tripwireDecls carries the rule's tripwire in both forms and the exit
// code contract every script shares. The codes stay apart so a run that
// never compared anything cannot read as a candidate that failed: 1 is
// the tripwire and nothing else.
const tripwireDecls = `

# The tripwire that came with the finding, in both forms: the sentence
# to read, and the comparison this script exits on. Exit 0 means it
# held, 1 means it tripped and the current model stays, 2 means the run
# could not answer. An empty metric means the rule's tripwire names
# nothing this script measures, so there is nothing to gate.
TRIPWIRE = {{TRIPWIRE_LITERAL}}
TRIPWIRE_METRIC = "{{TRIPWIRE_METRIC}}"
TRIPWIRE_COMPARE = "{{TRIPWIRE_COMPARE}}"
TRIPWIRE_THRESHOLD = {{TRIPWIRE_THRESHOLD}}


def fail(message):
    """Exit 2: nothing was compared, so this is no answer about models."""
    print(message, file=sys.stderr)
    sys.exit(2)


def check_tripwire(measured):
    """Print the tripwire and return the exit code it implies."""
    print("tripwire: " + TRIPWIRE)
    if not TRIPWIRE_METRIC:
        return 0
    if TRIPWIRE_METRIC not in measured:
        print("this script does not measure %s" % TRIPWIRE_METRIC,
              file=sys.stderr)
        return 2
    value = measured[TRIPWIRE_METRIC]
    if TRIPWIRE_COMPARE == "below":
        tripped = value < TRIPWIRE_THRESHOLD
    else:
        tripped = value > TRIPWIRE_THRESHOLD
    print("tripwire: %s is %.1f%%, trips %s %g%%: %s"
          % (TRIPWIRE_METRIC, value, TRIPWIRE_COMPARE, TRIPWIRE_THRESHOLD,
             "tripped, keep the current model" if tripped else "held"))
    return 1 if tripped else 0
`

// chatDecls holds the constants every chat script bakes in: the two
// model ids, their catalog prices, and the judge prompt.
const chatDecls = `
CURRENT = "{{CURRENT}}"
CANDIDATE = "{{CANDIDATE}}"

# Prices are US dollars per million tokens, from the overwater catalog.
CURRENT_IN = {{CURRENT_IN}}
CURRENT_OUT = {{CURRENT_OUT}}
CANDIDATE_IN = {{CANDIDATE_IN}}
CANDIDATE_OUT = {{CANDIDATE_OUT}}

JUDGE_PROMPT = """Two models answered the same prompt. Are the two
answers equivalent for the task? Start your reply with yes or no.

Prompt:
%s

Answer A:
%s

Answer B:
%s"""
` + tripwireDecls

// chatMain is the scoring half shared by every chat template. The
// provider half must define make_client() and ask(client, model,
// system, prompt); ask returns the reply text, the latency in
// milliseconds, and the input and output token counts.
const chatMain = `

def load_rows(path):
    rows = []
    try:
        with open(path) as handle:
            for line in handle:
                if line.strip():
                    rows.append(json.loads(line))
    except (OSError, ValueError) as err:
        fail("cannot read %s: %s" % (path, err))
    return rows


def track(stats, ms, tokens_in, tokens_out):
    stats["ms"].append(ms)
    stats["in"] += tokens_in
    stats["out"] += tokens_out


def report(model, stats, price_in, price_out):
    if not stats["ms"]:
        return 0.0
    mean = sum(stats["ms"]) / len(stats["ms"])
    cost = (stats["in"] * price_in + stats["out"] * price_out) / 1e6
    print("%s: mean latency %.0f ms, tokens %d in / %d out,"
          " estimated cost $%.4f"
          % (model, mean, stats["in"], stats["out"], cost))
    return cost


def says_yes(verdict):
    words = verdict.strip().lower().split()
    return bool(words) and words[0].strip(".,;:!") == "yes"


def main():
    if len(sys.argv) not in (2, 3):
        fail("usage: python3 " + sys.argv[0] + " prompts.jsonl [judge-model]")
    judge = sys.argv[2] if len(sys.argv) == 3 else ""
    rows = load_rows(sys.argv[1])
    if not rows:
        fail("no prompts in " + sys.argv[1])
    if len(rows) < 30:
        print("warning: only %d prompts; agreement on a small set is"
              " a smoke test, not evidence" % len(rows))
    client = make_client()
    cur_stats = {"ms": [], "in": 0, "out": 0}
    cand_stats = {"ms": [], "in": 0, "out": 0}
    judge_stats = {"ms": [], "in": 0, "out": 0}
    exact = 0
    judged = 0
    for i, row in enumerate(rows):
        a, ms, tokens_in, tokens_out = ask(
            client, CURRENT, row.get("system", ""), row["prompt"])
        track(cur_stats, ms, tokens_in, tokens_out)
        b, ms, tokens_in, tokens_out = ask(
            client, CANDIDATE, row.get("system", ""), row["prompt"])
        track(cand_stats, ms, tokens_in, tokens_out)
        if a == b:
            exact += 1
            judged += 1
            continue
        print("prompt %d disagrees:" % i)
        print("  %s: %r" % (CURRENT, a[:160]))
        print("  %s: %r" % (CANDIDATE, b[:160]))
        if judge:
            verdict, ms, tokens_in, tokens_out = ask(
                client, judge, "", JUDGE_PROMPT % (row["prompt"], a, b))
            track(judge_stats, ms, tokens_in, tokens_out)
            if says_yes(verdict):
                judged += 1
                print("  judge %s: equivalent" % judge)
            else:
                print("  judge %s: not equivalent" % judge)
    label = "exact agreement" if judge else "agreement"
    print("%s: %d/%d (%.1f%%)"
          % (label, exact, len(rows), 100.0 * exact / len(rows)))
    if judge:
        print("judged agreement: %d/%d (%.1f%%)"
              % (judged, len(rows), 100.0 * judged / len(rows)))
    total = report(CURRENT, cur_stats, CURRENT_IN, CURRENT_OUT)
    total += report(CANDIDATE, cand_stats, CANDIDATE_IN, CANDIDATE_OUT)
    print("total estimated cost of this eval run: $%.4f" % total)
    if judge_stats["ms"]:
        print("judge tokens %d in / %d out are outside the estimate;"
              " this script has no baked in price for %s"
              % (judge_stats["in"], judge_stats["out"], judge))
    # With a judge, judged agreement is the number the tripwire reads:
    # it counts the exact matches plus the ones the judge waved through.
    agreed = judged if judge else exact
    return check_tripwire({"agreement": 100.0 * agreed / len(rows)})


if __name__ == "__main__":
    try:
        code = main()
    except Exception:
        # A client or network failure is not a verdict on the candidate.
        traceback.print_exc()
        fail("the run failed before it could answer")
    sys.exit(code)
`

const anthropicTemplate = `#!/usr/bin/env python3
# Generated by overwater for {{RULE}} at {{SITE}}.
# Compares {{CURRENT}} against {{CANDIDATE}} on your own prompts.
# Tripwire: {{TRIPWIRE}}
# Exits 0 when the tripwire held, 1 when it tripped, 2 when the run
# could not answer, so CI can read the exit code as the verdict.
#
# This script calls the Anthropic API with your key. overwater itself
# never calls any model API; you run this, on your terms.
#
# Usage:
#   pip install anthropic
#   export ANTHROPIC_API_KEY=your-key
#   python3 {{SCRIPT}} prompts.jsonl [judge-model]
#
# prompts.jsonl holds one JSON object per line:
#   {"prompt": "the user message", "system": "optional system prompt"}
# Use real production prompts; invented ones prove nothing.
#
# The optional judge-model is asked, on each disagreement, whether the
# two answers are equivalent for the task; judged agreement then prints
# next to exact agreement.

import json
import sys
import time
import traceback

import anthropic
` + chatDecls + `

def make_client():
    return anthropic.Anthropic()


def ask(client, model, system, prompt):
    kwargs = {
        "model": model,
        "max_tokens": 512,
        "messages": [{"role": "user", "content": prompt}],
    }
    if system:
        kwargs["system"] = system
    start = time.monotonic()
    message = client.messages.create(**kwargs)
    ms = (time.monotonic() - start) * 1000.0
    parts = []
    for block in message.content:
        text = getattr(block, "text", None)
        if text:
            parts.append(text)
    usage = getattr(message, "usage", None)
    tokens_in = getattr(usage, "input_tokens", 0) or 0
    tokens_out = getattr(usage, "output_tokens", 0) or 0
    return "\n".join(parts).strip(), ms, tokens_in, tokens_out
` + chatMain

const openaiTemplate = `#!/usr/bin/env python3
# Generated by overwater for {{RULE}} at {{SITE}}.
# Compares {{CURRENT}} against {{CANDIDATE}} on your own prompts.
# Tripwire: {{TRIPWIRE}}
# Exits 0 when the tripwire held, 1 when it tripped, 2 when the run
# could not answer, so CI can read the exit code as the verdict.
#
# This script calls the OpenAI API with your key. overwater itself
# never calls any model API; you run this, on your terms.
#
# Usage:
#   pip install openai
#   export OPENAI_API_KEY=your-key
#   python3 {{SCRIPT}} prompts.jsonl [judge-model]
#
# prompts.jsonl holds one JSON object per line:
#   {"prompt": "the user message", "system": "optional system prompt"}
# Use real production prompts; invented ones prove nothing.
#
# The optional judge-model is asked, on each disagreement, whether the
# two answers are equivalent for the task; judged agreement then prints
# next to exact agreement.

import json
import sys
import time
import traceback

from openai import OpenAI
` + chatDecls + `

def make_client():
    return OpenAI()


def ask(client, model, system, prompt):
    messages = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": prompt})
    start = time.monotonic()
    resp = client.chat.completions.create(
        model=model,
        max_completion_tokens=512,
        messages=messages,
    )
    ms = (time.monotonic() - start) * 1000.0
    usage = getattr(resp, "usage", None)
    tokens_in = getattr(usage, "prompt_tokens", 0) or 0
    tokens_out = getattr(usage, "completion_tokens", 0) or 0
    text = (resp.choices[0].message.content or "").strip()
    return text, ms, tokens_in, tokens_out
` + chatMain

// compatTemplate serves every provider whose chat endpoint speaks the
// OpenAI protocol; the base URL and key variable are the only deltas.
const compatTemplate = `#!/usr/bin/env python3
# Generated by overwater for {{RULE}} at {{SITE}}.
# Compares {{CURRENT}} against {{CANDIDATE}} on your own prompts.
# Tripwire: {{TRIPWIRE}}
# Exits 0 when the tripwire held, 1 when it tripped, 2 when the run
# could not answer, so CI can read the exit code as the verdict.
#
# This script calls the {{PROVIDER}} API at {{BASE_URL}}, which speaks
# the OpenAI chat protocol, with your key. overwater itself never
# calls any model API; you run this, on your terms.
#
# Usage:
#   pip install openai
#   export {{ENV_VAR}}=your-key
#   python3 {{SCRIPT}} prompts.jsonl [judge-model]
#
# prompts.jsonl holds one JSON object per line:
#   {"prompt": "the user message", "system": "optional system prompt"}
# Use real production prompts; invented ones prove nothing.
#
# The optional judge-model is asked, on each disagreement, whether the
# two answers are equivalent for the task; judged agreement then prints
# next to exact agreement.

import json
import os
import sys
import time
import traceback

from openai import OpenAI
` + chatDecls + `

def make_client():
    key = os.environ.get("{{ENV_VAR}}")
    if not key:
        fail("set {{ENV_VAR}} to your API key")
    return OpenAI(base_url="{{BASE_URL}}", api_key=key)


def ask(client, model, system, prompt):
    messages = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": prompt})
    start = time.monotonic()
    resp = client.chat.completions.create(
        model=model,
        max_tokens=512,
        messages=messages,
    )
    ms = (time.monotonic() - start) * 1000.0
    usage = getattr(resp, "usage", None)
    tokens_in = getattr(usage, "prompt_tokens", 0) or 0
    tokens_out = getattr(usage, "completion_tokens", 0) or 0
    text = (resp.choices[0].message.content or "").strip()
    return text, ms, tokens_in, tokens_out
` + chatMain

const googleTemplate = `#!/usr/bin/env python3
# Generated by overwater for {{RULE}} at {{SITE}}.
# Compares {{CURRENT}} against {{CANDIDATE}} on your own prompts.
# Tripwire: {{TRIPWIRE}}
# Exits 0 when the tripwire held, 1 when it tripped, 2 when the run
# could not answer, so CI can read the exit code as the verdict.
#
# This script calls the Google Gemini API with your key. overwater
# itself never calls any model API; you run this, on your terms.
#
# Usage:
#   pip install google-genai
#   export GEMINI_API_KEY=your-key
#   python3 {{SCRIPT}} prompts.jsonl [judge-model]
#
# prompts.jsonl holds one JSON object per line:
#   {"prompt": "the user message", "system": "optional system prompt"}
# Use real production prompts; invented ones prove nothing.
#
# The optional judge-model is asked, on each disagreement, whether the
# two answers are equivalent for the task; judged agreement then prints
# next to exact agreement.

import json
import sys
import time
import traceback

from google import genai
` + chatDecls + `

def make_client():
    return genai.Client()


def ask(client, model, system, prompt):
    config = {"max_output_tokens": 512}
    if system:
        config["system_instruction"] = system
    start = time.monotonic()
    resp = client.models.generate_content(
        model=model, contents=prompt, config=config)
    ms = (time.monotonic() - start) * 1000.0
    usage = getattr(resp, "usage_metadata", None)
    tokens_in = getattr(usage, "prompt_token_count", 0) or 0
    tokens_out = getattr(usage, "candidates_token_count", 0) or 0
    return (resp.text or "").strip(), ms, tokens_in, tokens_out
` + chatMain

const cohereTemplate = `#!/usr/bin/env python3
# Generated by overwater for {{RULE}} at {{SITE}}.
# Compares {{CURRENT}} against {{CANDIDATE}} on your own prompts.
# Tripwire: {{TRIPWIRE}}
# Exits 0 when the tripwire held, 1 when it tripped, 2 when the run
# could not answer, so CI can read the exit code as the verdict.
#
# This script calls the Cohere API with your key. overwater itself
# never calls any model API; you run this, on your terms.
#
# Usage:
#   pip install cohere
#   export COHERE_API_KEY=your-key
#   python3 {{SCRIPT}} prompts.jsonl [judge-model]
#
# prompts.jsonl holds one JSON object per line:
#   {"prompt": "the user message", "system": "optional system prompt"}
# Use real production prompts; invented ones prove nothing.
#
# The optional judge-model is asked, on each disagreement, whether the
# two answers are equivalent for the task; judged agreement then prints
# next to exact agreement.

import json
import os
import sys
import time
import traceback

import cohere
` + chatDecls + `

def make_client():
    key = os.environ.get("COHERE_API_KEY")
    if not key:
        fail("set COHERE_API_KEY to your API key")
    return cohere.ClientV2(api_key=key)


def ask(client, model, system, prompt):
    messages = []
    if system:
        messages.append({"role": "system", "content": system})
    messages.append({"role": "user", "content": prompt})
    start = time.monotonic()
    resp = client.chat(model=model, messages=messages, max_tokens=512)
    ms = (time.monotonic() - start) * 1000.0
    parts = []
    for block in resp.message.content or []:
        text = getattr(block, "text", None)
        if text:
            parts.append(text)
    usage = getattr(getattr(resp, "usage", None), "tokens", None)
    tokens_in = int(getattr(usage, "input_tokens", 0) or 0)
    tokens_out = int(getattr(usage, "output_tokens", 0) or 0)
    return "\n".join(parts).strip(), ms, tokens_in, tokens_out
` + chatMain

const embeddingTemplate = `#!/usr/bin/env python3
# Generated by overwater for {{RULE}} at {{SITE}}.
# Compares {{CURRENT}} against {{CANDIDATE}} as retrieval embeddings.
# Tripwire: {{TRIPWIRE}}
# Exits 0 when the tripwire held, 1 when it tripped, 2 when the run
# could not answer, so CI can read the exit code as the verdict.
#
# Different embedding models live in different vector spaces, so their
# vectors cannot be compared directly. Behavior can be: embed every
# text with both models and check whether each text's nearest neighbor
# stays the same. Ten or more varied texts make the number meaningful.
#
# Usage:
#   pip install openai
#   export OPENAI_API_KEY=your-key
#   python3 {{SCRIPT}} prompts.jsonl [pairs.jsonl]
#
# prompts.jsonl holds one JSON object per line: {"prompt": "chunk text"}
#
# The optional pairs.jsonl holds one JSON object per line:
#   {"query": "a search query", "relevant": "the text it should find"}
# When given, both models also get recall at 3: whether each query's
# paired relevant text lands in its top 3 among all relevant texts.

import json
import math
import sys
import traceback

from openai import OpenAI

CURRENT = "{{CURRENT}}"
CANDIDATE = "{{CANDIDATE}}"
` + tripwireDecls + `

def load_column(path, key):
    rows = []
    try:
        with open(path) as handle:
            for line in handle:
                if line.strip():
                    rows.append(json.loads(line)[key])
    except (OSError, ValueError, KeyError) as err:
        fail("cannot read %s: %s" % (path, err))
    return rows


def embed_all(client, model, texts):
    resp = client.embeddings.create(model=model, input=texts)
    return [item.embedding for item in resp.data]


def cosine(a, b):
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(x * x for x in b))
    if not na or not nb:
        return 0.0
    return dot / (na * nb)


def nearest(i, vectors):
    best, score = -1, -2.0
    for j, v in enumerate(vectors):
        if j == i:
            continue
        s = cosine(vectors[i], v)
        if s > score:
            best, score = j, s
    return best


def top3(query_vector, vectors):
    order = sorted(range(len(vectors)),
                   key=lambda j: cosine(query_vector, vectors[j]),
                   reverse=True)
    return order[:3]


def recall_at_3(client, model, queries, relevants):
    query_vectors = embed_all(client, model, queries)
    relevant_vectors = embed_all(client, model, relevants)
    hits = 0
    for i in range(len(queries)):
        if i in top3(query_vectors[i], relevant_vectors):
            hits += 1
    return 100.0 * hits / len(queries)


def pairs_report(client, path):
    queries = load_column(path, "query")
    relevants = load_column(path, "relevant")
    if len(queries) < 4:
        fail("need at least 4 pairs for recall at 3 to mean anything")
    if len(queries) < 30:
        print("warning: only %d pairs; recall on a small set is"
              " a smoke test, not evidence" % len(queries))
    cur = recall_at_3(client, CURRENT, queries, relevants)
    cand = recall_at_3(client, CANDIDATE, queries, relevants)
    print("recall at 3, %s: %.1f%%" % (CURRENT, cur))
    print("recall at 3, %s: %.1f%%" % (CANDIDATE, cand))
    print("recall at 3 delta, candidate minus current: %+.1f points"
          % (cand - cur))


def main():
    if len(sys.argv) not in (2, 3):
        fail("usage: python3 " + sys.argv[0] + " prompts.jsonl [pairs.jsonl]")
    texts = load_column(sys.argv[1], "prompt")
    if len(texts) < 3:
        fail("need at least 3 texts to compare neighborhoods")
    if len(texts) < 30:
        print("warning: only %d texts; agreement on a small set is"
              " a smoke test, not evidence" % len(texts))
    client = OpenAI()
    current = embed_all(client, CURRENT, texts)
    candidate = embed_all(client, CANDIDATE, texts)
    agree = 0
    for i in range(len(texts)):
        if nearest(i, current) == nearest(i, candidate):
            agree += 1
    pct = 100.0 * agree / len(texts)
    print("nearest neighbor agreement: %d/%d (%.1f%%)" % (agree, len(texts), pct))
    if len(sys.argv) == 3:
        pairs_report(client, sys.argv[2])
    return check_tripwire({"nearest_neighbor_agreement": pct})


if __name__ == "__main__":
    try:
        code = main()
    except Exception:
        # A client or network failure is not a verdict on the candidate.
        traceback.print_exc()
        fail("the run failed before it could answer")
    sys.exit(code)
`
