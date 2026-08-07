<?php

use OpenAI\Client;

// One turn of the resolution loop; $scratchpad already holds tool output.
function agent_step(Client $client, array $scratchpad, array $tools): array
{
    $response = $client->chat()->create([
        "model" => "gpt-4.1",
        "max_tokens" => 2500,
        "tools" => $tools,
        "messages" => $scratchpad,
    ]);

    return $response->toArray();
}
