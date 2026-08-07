<?php

use OpenAI\Client;

function permit_fields(Client $client, string $permit): string
{
    $response = $client->chat()->create([
        "model" => "gpt-4.1-nano",
        "temperature" => 0,
        "max_tokens" => 300,
        "messages" => [
            ["role" => "system", "content" => "Copy permit_number, issued_to, parcel_id, and expiry off the permit into JSON."],
            ["role" => "user", "content" => $permit],
        ],
    ]);

    return $response->choices[0]->message->content;
}
