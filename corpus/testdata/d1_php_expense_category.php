<?php

use OpenAI\Client;

function categorize_expense(Client $client, string $line): string
{
    $response = $client->chat()->create([
        "model" => "gpt-4o-mini",
        "temperature" => 0,
        "max_tokens" => 6,
        "messages" => [
            ["role" => "system", "content" => "Pick one expense category: travel, meals, software, hardware, other."],
            ["role" => "user", "content" => $line],
        ],
    ]);

    return trim($response->choices[0]->message->content);
}
