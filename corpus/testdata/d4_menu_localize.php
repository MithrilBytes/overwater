<?php

use OpenAI\Client;

function localize_menu(Client $client, string $menu, string $target): string
{
    $response = $client->chat()->create([
        "model" => "gpt-4o-mini",
        "temperature" => 0,
        "max_tokens" => 1500,
        "messages" => [
            ["role" => "system", "content" => "Translate the menu into the target language. Keep dish names that are proper nouns in the original."],
            ["role" => "user", "content" => $target . "\n" . $menu],
        ],
    ]);

    return $response->choices[0]->message->content;
}
