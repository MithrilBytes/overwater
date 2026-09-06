<?php

// From aschmelyun/subvert, src/app/Actions/GenerateSummary.php, one stage of
// the Laravel pipeline that runs over a video's transcript chunks. The second
// pass that condenses the per chunk summaries is dropped so one model string
// is left; the chunk summaries are joined instead.

namespace App\Actions;

use App\Enums\Status;
use App\Models\Process;
use OpenAI\Laravel\Facades\OpenAI;

class GenerateSummary
{
    public function handle(Process $process, \Closure $next)
    {
        $process->update([
            'status' => Status::PROCESSING_SUMMARY
        ]);

        $option = filter_var($process->options['summary'], FILTER_VALIDATE_BOOLEAN);
        if (!$option) {
            return $next($process);
        }

        try {
            $completedSummaryChunks = [];

            foreach($process->transcriptChunks as $chunk) {
                $completedSummaryChunks[] = $this->getSummary($chunk);
            }

            $process->update([
                'summary' => implode(' ', $completedSummaryChunks)
            ]);
        } catch (\Exception $e) {
            $process->update([
                'status' => Status::ERRORED,
                'error' => $e->getMessage(),
            ]);

            return;
        }

        return $next($process);
    }

    private function getSummary($subtitles)
    {
        $summary = OpenAI::chat()->create([
            'model' => 'gpt-3.5-turbo',
            'temperature' => 0.2,
            'messages' => [
                [
                    'role' => 'system',
                    'content' => 'You are a video editor. You will be given subtitles of a video. You need to summarize the video in a concise manner, as a single paragraph, and no more than 5 sentences. Provide back only the summary text, nothing before and nothing after.'
                ],
                [
                    'role' => 'user',
                    'content' => implode("\n", $subtitles)
                ]
            ]
        ]);

        $completedSummary = '';
        foreach($summary->choices as $choice) {
            $completedSummary .= $choice->message->content;
        }

        return $completedSummary;
    }
}
