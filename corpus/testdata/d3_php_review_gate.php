<?php

use OpenAI\Client;

function review_allowed(Client $client, string $review): bool
{
    $response = $client->chat()->create([
        "model" => "gpt-4o-mini",
        "temperature" => 0,
        "max_tokens" => 3,
        "messages" => [
            ["role" => "system", "content" => "Block reviews that contain profanity, competitor spam, or contact details. Answer allow or block."],
            ["role" => "user", "content" => $review],
        ],
    ]);

    return trim($response->choices[0]->message->content) === "allow";
}
