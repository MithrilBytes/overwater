<?php

use OpenAI\Client;

function review_roundup(Client $client, array $reviews): string
{
    $response = $client->chat()->create([
        "model" => "gpt-4o-mini",
        "max_tokens" => 500,
        "messages" => [
            ["role" => "system", "content" => "Sum up what customers say about this product in one paragraph, naming the two complaints that repeat most."],
            ["role" => "user", "content" => implode("\n", $reviews)],
        ],
    ]);

    return $response->choices[0]->message->content;
}
