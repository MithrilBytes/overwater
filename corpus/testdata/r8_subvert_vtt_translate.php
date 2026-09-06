<?php

// From aschmelyun/subvert, src/app/Actions/TranslateSubtitles.php, one stage
// of the Laravel pipeline that runs over a video's transcript chunks. The
// target language comes off the process options; the call has no
// temperature or cap in the repository and none is added.

namespace App\Actions;

use App\Enums\Status;
use App\Models\Process;
use OpenAI\Laravel\Facades\OpenAI;

class TranslateSubtitles
{
    public function handle(Process $process, \Closure $next)
    {
        $process->update([
            'status' => Status::TRANSLATING_SUBTITLES
        ]);

        if ($process->options['language'] === 'default') {
            return $next($process);
        }

        try {
            $completedTranslationChunks = [];

            foreach($process->transcriptChunks as $chunk) {
                $completedTranslationChunks[] = $this->translateChunk($chunk, $process->options['language']);
            }

            $process->update([
                'transcript' => implode("\n", $completedTranslationChunks)
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

    private function translateChunk($chunk, $language)
    {
        $summary = OpenAI::chat()->create([
            'model' => 'gpt-3.5-turbo',
            'messages' => [
                [
                    'role' => 'system',
                    'content' => "Translate the vtt subtitles provided to {$language}. Provide back the translated vtt file, including the original timestamps."
                ],
                [
                    'role' => 'user',
                    'content' => implode("\n", $chunk)
                ]
            ]
        ]);

        $completedTranslation = '';
        foreach($summary->choices as $choice) {
            $completedTranslation .= $choice->message->content;
        }

        return $completedTranslation;
    }
}
