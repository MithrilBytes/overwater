<?php

use OpenAI\Client;

// The inbox widget calls this before it opens a conversation.
function chat_route(Client $client, string $message): string
{
    $response = $client->chat()->create([
        "model" => "gpt-4o-mini",
        "temperature" => 0,
        "max_tokens" => 5,
        "messages" => [
            ["role" => "system", "content" => "Which queue should this message go to? Answer sales, support, or billing."],
            ["role" => "user", "content" => $message],
        ],
    ]);

    return trim($response->choices[0]->message->content);
}
