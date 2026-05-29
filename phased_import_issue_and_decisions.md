# Lenos protocol decision

## Current protocol

Agent only emit bash for all responses. even when they want speak human language

## Reasoning for current design

1. in one standard agent session, 98% token are apply on the bash call, others are interactive with human in english/...
2. when we keep 2 protocol, like use special wrapper, we are rebuild a tiny tool call protocol which use <cmd> </cmd> tag
3. any <cmd> or <run lang=bash> wrapper protocol means under this protocol, agent cannot develop lenos itself, means they only able to handle proj which doesnt include <cmd> inside their codebase

## Issues we are facing
we able to handle "this response is only natural language" (we show narrate content to human) or "this response is only bash" (we run it)


but if llm forget or ignore our system prompt, they may use 

Now I need to remove the duplicate `FullTranscriptView` block from `VoiceNoteContent`. Let me also update the imports to remove the unused one. Let me first check what imports VoiceNoteContent uses and which are still needed after removing the block.

I also need to update `VoiceNotePlayer` to pass the new props to `VoiceNoteContent`. Let me do both.

First, let me remove the duplicate `FullTranscriptView` block from `VoiceNoteContent`:# Now remove the duplicate FullTranscriptView block from VoiceNoteContent
cat <<'EOF' | src edit ./apps/web/src/components/VoiceNoteContent.tsx --section Z6
===BEFORE===
			{/* Full transcript view */}
			{showFullTranscript && (


or

Now I need to remove the duplicate `FullTranscriptView` block from `VoiceNoteContent`. Let me also update the imports to remove the unused one. Let me first check what imports VoiceNoteContent uses and which are still needed after removing the block.

I also need to update `VoiceNotePlayer` to pass the new props to `VoiceNoteContent`. Let me do both.

First, let me remove the duplicate `FullTranscriptView` block from `VoiceNoteContent`:# Now remove the duplicate FullTranscriptView block from VoiceNoteContent

```bash
cat <<'EOF' | src edit ./apps/web/src/components/VoiceNoteContent.tsx --section Z6
===BEFORE===
			{/* Full transcript view */}
			{showFullTranscript && (
...


and at this time, we cannot know how to handle it

since it merge the natural language and the action.

which is also the main failure and the reason to doubt of current bash-first protocol.

