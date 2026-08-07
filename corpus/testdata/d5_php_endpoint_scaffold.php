<?php

use OpenAI\Client;

function scaffold_endpoint(Client $client, string $spec): string
{
    $response = $client->chat()->create([
        "model" => "gpt-4.1",
        "max_tokens" => 2500,
        "temperature" => 0.2,
        "messages" => [
            ["role" => "system", "content" => "Write the Laravel controller and route for the endpoint described. Return PHP source only."],
            ["role" => "user", "content" => $spec],
        ],
    ]);

    return $response->choices[0]->message->content;
}
