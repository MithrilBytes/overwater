<?php

$client = OpenAI::client(getenv('OPENAI_API_KEY'));

function extractInvoiceFields($client, string $pdfText): array
{
    $response = $client->chat()->create([
        'model' => 'gpt-5.1',
        'messages' => [
            ['role' => 'system', 'content' => 'Extract vendor, invoice number, due date, and line item totals as JSON.'],
            ['role' => 'user', 'content' => $pdfText],
        ],
        'response_format' => ['type' => 'json_object'],
    ]);

    return json_decode($response->choices[0]->message->content, true);
}
