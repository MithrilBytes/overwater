<?php

$client = OpenAI::factory()
    ->withApiKey(getenv('MISTRAL_API_KEY'))
    ->withBaseUri('https://api.mistral.ai/v1')
    ->make();

function translateListing($client, string $listing): string
{
    $response = $client->chat()->create([
        'model' => 'mistral-small-2506',
        'messages' => [
            ['role' => 'system', 'content' => 'Translate product listings into German. Keep prices and sizes unchanged.'],
            ['role' => 'user', 'content' => $listing],
        ],
    ]);

    return $response->choices[0]->message->content;
}
