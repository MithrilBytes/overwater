<?php

use OpenAI\Client;

function lecture_to_text(Client $client, string $path): string
{
    $response = $client->audio()->transcribe([
        "model" => "gpt-4o-mini",
        "file" => fopen($path, "r"),
        "response_format" => "text",
        "language" => "en",
    ]);

    return $response->text;
}
