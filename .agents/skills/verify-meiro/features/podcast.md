# Podcasts

A user opens a podcast show, reads its episodes and plays one. An episode left part-way through starts where it stopped the next time it plays, including after a restart.

## Sub-features

- `podcast-open` opens a show from search as a page with its description and newest episodes.
- `podcast-episodes` shows each episode's description, date and length, and loads older episodes.
- `podcast-play` plays an episode like a track, with the player bar, seeking and queue.
- `podcast-resume` remembers a part-heard episode across track changes and app restarts.

## How to get to it (user POV)

- Search for a show, then open its result.
- Click an episode to play it. Use the player bar or queue to control playback or choose another episode.
- Leave an episode part-way through, then play it again. It resumes at the saved position.

## Driving it with the headless tester

Preconditions:

- Run `scripts/doctor.sh` first.
- `TestEveryPageLoadsAndDraws` opens `/podcast/MPSPPLshow` and checks the show title, description and an episode.
- `youtube.TestPodcastShowListsItsEpisodes` checks the show's description, episode order and metadata, and that Show more uses the episode shelf continuation rather than the filter reload token.
- `TestAPodcastEpisodeComesBackWhereItStopped` uses a real tone through ffmpeg, plays an episode, changes tracks, reads its saved position from `playback.json`, then plays the episode again and checks that it resumes.

## Gotchas

- Episode duration comes from the show's episode row. Search-result rows do not provide it.
- Resume positions are stored locally by Meiro; they do not use YouTube's progress or sync across devices.
- Native verification uses a signed-out YouTube Music session. Real-account behavior and MPRIS remain unverified in the orb.
