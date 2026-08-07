<?php

use OpenAI\Client;

function reorder_products(Client $client, string $query, string $listing): string
{
    $response = $client->chat()->create([
        "model" => "gpt-4o-mini",
        "temperature" => 0,
        "max_tokens" => 120,
        "messages" => [
            ["role" => "system", "content" => "Sort the candidate products by how well they match the shopper's query. Return the sku list in order, nothing else."],
            ["role" => "user", "content" => $query . "\n" . $listing],
        ],
    ]);

    return $response->choices[0]->message->content;
}
