package main

import (
	"context"
	"log"
	"slices"
	"time"

	"github.com/elianiva/meiro/internal/playstate"
	"github.com/elianiva/meiro/youtube"
)

// episodeProgressSync owns one account progress session. It is main-thread
// state; network requests capture its pointer and publish results through
// app.update.
type episodeProgressSync struct {
	item         youtube.MusicItem
	client       *youtube.Client
	session      *youtube.PlaybackSession
	duration     time.Duration
	lastReported time.Duration
	lastPosition time.Duration
	lastSample   time.Time
	pendingTo    time.Duration
	seekVersion  uint64
	pending      bool
	finished     bool
	finalSent    bool
	finishAt     time.Duration
}

// episodeProgressInterval bounds how often the account receives a heartbeat.
const episodeProgressInterval = 20 * time.Second

// episodeProgressTimeout bounds starting a tracking session or sending a
// watch-time interval.
const episodeProgressTimeout = 15 * time.Second

// reportPlayback tells YouTube Music the signed-in account started item, so
// it lands in the history that the recommendations are made from. Nothing on
// screen depends on it, and a failure costs only a worse suggestion.
func (a *app) reportPlayback(item youtube.MusicItem) {
	if !a.signedIn() {
		return
	}
	client := a.client()
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), upNextTimeout)
		defer cancel()
		if err := client.ReportPlayback(ctx, item.VideoID); err != nil {
			log.Printf("playback: could not report video_id=%s: %v", item.VideoID, err)
		}
	})
}

// episodeProgressPosition turns YouTube's percentage into a resume position
// after audio resolution supplies the actual duration. Ignore nearly-new and
// nearly-finished positions, as the local episode resume does.
func episodeProgressPosition(item youtube.MusicItem, duration time.Duration) time.Duration {
	if !item.IsEpisode() || item.ProgressPercent <= 0 || item.ProgressPercent >= 100 || duration <= 0 {
		return 0
	}
	position := time.Duration(int64(duration) * int64(item.ProgressPercent) / 100)
	if position < episodeStartGuard || duration-position < episodeEndGuard {
		return 0
	}
	return position
}

// beginEpisodeProgress starts the account's tracking session without holding
// up audio. A result for an episode that has already ended is still used to
// send its final interval, but cannot replace the current episode's session.
func (a *app) beginEpisodeProgress(item youtube.MusicItem, position, duration time.Duration) {
	if !a.signedIn() || !item.IsEpisode() {
		return
	}
	if a.episodeProgress != nil {
		a.finishEpisodeProgress(false)
	}
	state := &episodeProgressSync{
		item: item, client: a.client(), duration: duration, lastReported: position,
		lastPosition: position, lastSample: time.Now(), seekVersion: a.seekVersion,
		pending: true,
	}
	a.episodeProgress = state
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), episodeProgressTimeout)
		defer cancel()
		session, err := state.client.BeginEpisodePlayback(ctx, item.VideoID, position, duration)
		a.update(func() {
			state.pending = false
			if err != nil {
				log.Printf("playback: could not start episode progress video_id=%s: %v", item.VideoID, err)
				if a.episodeProgress == state {
					a.episodeProgress = nil
				}
				return
			}
			state.session = session
			if state.finished {
				a.sendEpisodeProgressFinal(state)
				return
			}
			if a.episodeProgress != state {
				return
			}
		})
	})
}

// syncEpisodeProgress sends a watch-time interval after each 20 seconds of
// actual playback. Position jumps and seeks reset its baseline, so skipped
// audio is never attributed to the account.
func (a *app) syncEpisodeProgress() {
	state := a.episodeProgress
	if state == nil || state.finished || state.session == nil || state.pending || !a.player.Playing() {
		return
	}
	now, position := time.Now(), a.player.Position()
	if state.seekVersion != a.seekVersion {
		state.seekVersion = a.seekVersion
		state.lastReported, state.lastPosition, state.lastSample = position, position, now
		return
	}
	elapsed := now.Sub(state.lastSample)
	advanced := position - state.lastPosition
	state.lastPosition, state.lastSample = position, now
	if advanced < 0 || advanced > elapsed+2*time.Second {
		state.lastReported = position
		return
	}
	if position-state.lastReported < episodeProgressInterval {
		return
	}
	from, to := state.lastReported, position
	state.pending = true
	state.pendingTo = to
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), episodeProgressTimeout)
		defer cancel()
		err := state.session.ReportWatchtime(ctx, from, to, state.duration, "playing", false)
		a.update(func() {
			state.pending = false
			state.pendingTo = 0
			if err != nil {
				log.Printf("playback: could not report episode progress video_id=%s: %v", state.item.VideoID, err)
			} else {
				state.lastReported = to
			}
			if state.finished {
				a.sendEpisodeProgressFinal(state)
			}
		})
	})
}

// finishEpisodeProgress closes the current interval when an episode pauses,
// ends or is replaced. During shutdown it waits briefly so the final position
// is not lost when the window's update loop stops.
func (a *app) finishEpisodeProgress(wait bool) {
	state := a.episodeProgress
	if state == nil {
		return
	}
	state.finished = true
	state.finishAt = max(a.playbackPosition(), state.lastReported)
	a.episodeProgress = nil
	if state.session == nil || state.pending {
		if !wait {
			return
		}
	}
	if state.session == nil || state.finalSent {
		return
	}
	from, to := state.lastReported, state.finishAt
	if state.pending && state.session != nil {
		from = max(from, state.pendingTo)
	}
	if to < from {
		to = from
	}
	if wait {
		state.finalSent = true
		result := make(chan error, 1)
		a.run(func() {
			ctx, cancel := context.WithTimeout(context.Background(), episodeProgressTimeout)
			defer cancel()
			result <- state.session.ReportWatchtime(ctx, from, to, state.duration, "paused", true)
		})
		if err := <-result; err != nil {
			log.Printf("playback: could not finish episode progress video_id=%s: %v", state.item.VideoID, err)
		}
		return
	}
	a.sendEpisodeProgressFinal(state)
}

// sendEpisodeProgressFinal sends the final interval once, including when the
// session-start or heartbeat request completed after the episode was left.
func (a *app) sendEpisodeProgressFinal(state *episodeProgressSync) {
	if state.session == nil || state.finalSent {
		return
	}
	from, to := state.lastReported, max(state.finishAt, state.lastReported)
	state.finalSent = true
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), episodeProgressTimeout)
		defer cancel()
		if err := state.session.ReportWatchtime(ctx, from, to, state.duration, "paused", true); err != nil {
			log.Printf("playback: could not finish episode progress video_id=%s: %v", state.item.VideoID, err)
		}
	})
}

// leaveEpisode keeps where listening stopped in the current track, when it is
// a podcast episode left part-way through. It runs as the track is left.
func (a *app) leaveEpisode() {
	item := a.current
	if !item.IsEpisode() {
		return
	}
	position := a.playbackPosition()
	a.takeEpisode(item.VideoID)
	if position < episodeStartGuard || (a.total > 0 && position > a.total-episodeEndGuard) {
		return
	}
	a.episodes = slices.Insert(a.episodes, 0, playstate.Episode{VideoID: item.VideoID, Position: position})
	a.episodes = a.episodes[:min(len(a.episodes), playstate.MaxEpisodes)]
}

// takeEpisode returns where listening to an episode stopped, or 0, and
// forgets it: from here the episode is the current track, whose place the
// saved queue keeps.
func (a *app) takeEpisode(videoID string) time.Duration {
	i := slices.IndexFunc(a.episodes, func(e playstate.Episode) bool { return e.VideoID == videoID })
	if i < 0 {
		return 0
	}
	position := a.episodes[i].Position
	a.episodes = slices.Delete(a.episodes, i, i+1)
	return position
}
