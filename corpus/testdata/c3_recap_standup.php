<?php

use Anthropic\Client;

$client = new Client(apiKey: getenv('ANTHROPIC_API_KEY'));

function recapStandup(Client $client, string $notes): string
{
    $message = $client->messages->create(
        model: 'claude-sonnet-5',
        maxTokens: 400,
        system: 'Digest the standup notes into three bullets: done, in progress, blocked.',
        messages: [['role' => 'user', 'content' => $notes]],
    );

    return $message->content[0]->text;
}
