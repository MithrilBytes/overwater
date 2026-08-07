<?php

use Anthropic\Anthropic;

function concierge_reply(Anthropic $client, array $turns): string
{
    $message = $client->messages()->create([
        "model" => "claude-haiku-4-5",
        "max_tokens" => 800,
        "system" => "You are the front desk assistant at a small hotel. Be warm, brief, and offer one suggestion.",
        "messages" => $turns,
    ]);

    return $message->content[0]->text;
}
