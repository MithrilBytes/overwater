<?php

use OpenAI\Client;

function photo_tags(Client $client, string $imageUrl): string
{
    $response = $client->chat()->create([
        "model" => "gpt-4o-mini",
        "max_tokens" => 150,
        "messages" => [
            [
                "role" => "user",
                "content" => [
                    ["type" => "text", "text" => "List what you can see in this product photo: item, colour, material, and setting."],
                    ["type" => "image_url", "image_url" => ["url" => $imageUrl]],
                ],
            ],
        ],
    ]);

    return $response->choices[0]->message->content;
}
