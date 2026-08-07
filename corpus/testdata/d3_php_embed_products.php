<?php

use OpenAI\Client;

function embed_products(Client $client, array $descriptions): array
{
    $response = $client->embeddings()->create([
        "model" => "text-embedding-3-small",
        "input" => $descriptions,
    ]);

    return array_map(fn($item) => $item->embedding, $response->embeddings);
}
