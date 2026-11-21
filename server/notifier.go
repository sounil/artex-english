package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/notify"
)

// Global settings keys (stored in the settings key-value table; no new table needed).
const (
	// settingNotifyEnabled is the master push switch. Default on: it is a one-click kill switch for maintenance,
	// not the enabling condition of the feature — the real condition is "is any channel configured".
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL is the external base URL used to build vulnerability detail back-links
	// (e.g. https://artex.example.com). When empty, messages carry no back-link button.
	// The project has no reusable external URL setting, so a new one is added here.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes is the digest mode period (minutes).
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick is the delivery engine's polling interval. 3 seconds bounds the engine's latency
	// and is the main source of delay between "vulnerability persisted" and "message reaches IM".
	notifyTick = 3 * time.Second
	// When notifyBackgroundDisabledEnv is set the server does not start the background delivery loop
	// (newNotifier(s).Run). Integration tests call stepRealtime/stepDigest directly
	// to verify dispatch results deterministically; a 3-second background loop processing
	// the same channels concurrently would make the counts timing-dependent. This is a
	// test-only switch; without the variable startup is unchanged (production behavior unaffected).
	notifyBackgroundDisabledEnv = "ARTEX_NOTIFY_BACKGROUND_DISABLED"
	// notifyLease is the lease duration when claiming deliveries. It must be well above the worst-case time of one delivery
	// (the notify package HTTP client timeout of 15 seconds), otherwise the same row could be delivered by two
	// dispatchers at once.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick caps the number of events dispatched per round, so enabling a channel for the first time
	// does not expand the whole historical backlog into delivery jobs at once.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest period.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick is the per-round delivery cap when a channel has no rate limit.
	// It exists to stop "a channel configured with no rate limit + a scan producing thousands of vulnerabilities" from
	// turning a single round into a long block.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick is the maximum number of deliveries per channel per round.
	//
	// This cap is derived from the **lease duration**: claimed rows get a lease (notifyLease = 3 minutes);
	// if a round delivers so many items serially that the worst case exceeds the lease, the lease expires before the last ones are sent.
	// Within one process this does not matter (Run is a single serial goroutine; ticks never re-enter), but when **two
	// processes share one database**, the other side re-claims rows with expired leases and sends them again, also
	// incrementing attempts twice and judging a delivery failed while the original process is still sending it.
	//
	// Value: 3-minute lease / 30-second per-send timeout = 6 would **use the lease exactly** with zero margin,
	// so it is not allowed; 5 leaves a worst case of 150 seconds with 30 seconds to spare. The relationship is
	// pinned by TestNotifyTickBudgetFitsWithinLease — changing notifyLease,
	// notifySendTimeout or this value makes that assertion fail.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout is the per-delivery timeout. It also determines the previous constant's value:
	// their product must not exceed notifyLease; see TestNotifyTickBudgetFitsWithinLease.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff is the retry backoff sequence, indexed by attempts so far.
// 3 chances (including the first) match db.MaxNotifyAttempts; change both together.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// Delivery failure/deferral reasons. Stored in notification_deliveries.last_error and shown verbatim
// via notify_api.go's delivery history response in the notification page's "Delivery history" table (delivery-list.tsx),
// so they are user-facing (diagnostic log.Printf lines are separate).
const (
	errDeliveryChannelKindUnregistered = "Unregistered channel type: %q"
	errDeliverySnapshotUnrenderable    = "Could not parse the event snapshot, so this vulnerability could not be turned into a message"
	errDeliveryChannelLengthCapped     = "This message reached the channel length limit, so only the first %d were sent and the rest are deferred to the next batch"
	errDeliveryNoDeliveredCount        = "The channel did not report the number of messages sent (delivered=%d)"
	errDeliveryRetryExhausted          = "Still failed after %d retries: %s"
	errDeliverySnapshotEmpty           = "The event snapshot for delivery item #%d is empty"
	errDeliverySnapshotParse           = "Failed to parse the event snapshot of delivery item #%d: %w"
	errDeliveryBatchAllUnparseable     = "Failed to parse all %d deliveries in the summary batch"
)

// Notifier is the vulnerability push delivery engine.
//
// It runs as an independent goroutine alongside Scheduler (see server.New). It deliberately does not reuse
// Scheduler's tick: push latency requirements (3 seconds) differ from the triggers' business cadence,
// and failures must not affect each other — a stuck push must not affect agent triggers.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu guards buckets. Channels are few and contention is low, so one mutex is enough;
	// a finer-grained structure is not worth it.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is a per-channel token bucket.
//
// A token bucket rather than a "count per minute then reset" window, because the latter has bad edge effects:
// sending 20 at the end of a window and 20 more the next instant is 40 within a second to the platform,
// which gets rate limited; a token bucket refills at a constant rate and naturally avoids such bursts.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until ctx ends. Started once by server.New.
func (n *Notifier) Run(ctx context.Context) {
	if n.pg == nil {
		return
	}
	t := time.NewTicker(notifyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.step(ctx)
		}
	}
}

// step runs one round: dispatch new events first, then deliver due jobs.
//
// Any failing step is only logged and never breaks the loop — notification failures must never escalate into process-level problems.
// Every tick is independent; the next round retries naturally.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf("[notify] failed to dispatch events: %v", err)
		return
	}
	channels, err := n.pg.ListNotificationChannels(ctx)
	if err != nil {
		log.Printf("[notify] failed to read channels: %v", err)
		return
	}
	baseURL := n.publicBaseURL()
	for _, ch := range channels {
		if !ch.IsEnabled() {
			continue
		}
		// The token bucket counts **messages** (equivalent to HTTP requests), not vulnerabilities.
		// In realtime mode they are the same (one vulnerability, one message); in digest mode a whole batch of vulnerabilities becomes
		// one message, so it consumes only one token.
		//
		// Both modes ask the token bucket first, then claim within the quota — the order cannot be reversed, or deliveries blocked
		// by the rate limit would already have consumed retry attempts.
		now := time.Now()
		if ch.Mode == db.NotifyModeDigest {
			tokens, claimLimit := digestTickPlan()
			if n.takeTokens(ch.ID, ch.RatePerMin, tokens, now) <= 0 {
				continue
			}
			n.stepDigest(ctx, ch, claimLimit, baseURL)
			continue
		}
		allow := n.takeTokens(ch.ID, ch.RatePerMin, notifyMaxSendsPerChannelPerTick, now)
		if allow <= 0 {
			continue
		}
		n.stepRealtime(ctx, ch, allow, baseURL)
	}
}

// digestTickPlan returns a digest channel's token cost and batch size upper bound for this round.
//
// The two return values have **different units**, which is exactly why this is a separate function:
//
//   - tokens is the message count. A batch of vulnerabilities becomes one message and one HTTP request, so it is always 1.
//     rate_per_min therefore still applies to digest (at most that many digest messages per minute).
//   - claimLimit is the maximum number of vulnerabilities in this batch. It is bounded only by memory, not the request budget.
//
// To make rate_per_min apply to digest, the per-round request budget
// (notifyMaxSendsPerChannelPerTick, derived from the lease) was once passed down directly as the batch size.
// As a result a channel with rate_per_min=20 refilled only 1 token per 3-second tick, so each digest
// message carried just 1 vulnerability — digest degraded into "realtime push with digest wording", readers got a stream of
// "1 new vulnerability in the last 30 minutes", and db.MaxDigestBatchSize was never reachable.
//
// This symptom is hard to spot in end-to-end tests (existing cases pass a large enough limit to
// stepDigest by hand, bypassing the quota computation in step), so the decision is centralized here and
// pinned directly by TestDigestTickPlanDecouplesBatchSizeFromSendBudget.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and delivers a channel's realtime jobs, one message per vulnerability.
func (n *Notifier) stepRealtime(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	deliveries, err := n.pg.ClaimRealtimeDeliveries(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim realtime deliveries channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(trMsg(errDeliveryChannelKindUnregistered), ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// A render failure is a local data problem; retrying will not help.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, err.Error())
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest aggregates a channel's pending deliveries into one message when the batch is due.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf("[notify] failed to evaluate digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(trMsg(errDeliveryChannelKindUnregistered), ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), err.Error())
		return
	}
	// Deliveries whose snapshot is broken and that did not make it into the message must be explicitly failed. Otherwise they stay
	// outside included, in neither the message nor the failure list — on a successful send their status would be missed by
	// the subsequent bulk marking and stay in sending forever, re-claimed repeatedly whenever the lease expires.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := errDeliverySnapshotUnrenderable
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf("[notify] failed to mark deliveries with bad snapshots channel=%s ids=%v: %v", ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf("[notify] skipped %d deliveries with unparsable snapshots channel=%d", len(skipped), ch.ID)
	}
	// Only the deliveries that made it into the message go to send: included[i] corresponds strictly to msg.Items[i],
	// and send relies on that correspondence to apply "the channel reported fitting the first K" to the correct delivery rows.
	n.send(ctx, channel, cfg, msg, included)
}

// send delivers and transitions state by result.
//
// One batch of deliveries (possibly dozens in digest mode) shares one send result: either delivered or the whole batch retries.
// No per-item retries — a digest is a single message, and resending part of it would break batch semantics.
//
// The only exception is **splitting caused by the channel length cap**: when the channel reports it fit only the first K items,
// items K+1 onwards must wait for the next batch rather than being marked successful too. Otherwise the cut-off
// vulnerabilities would be in neither the message nor the failure list and vanish entirely.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	// Cap each delivery so one stuck channel cannot hold up all remaining channels in this round.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// The channel cannot report more items than deliveries; if it happens the render layer miscounted,
			// so treat it as all delivered and log the problem — better than corrupting the records.
			log.Printf("[notify] channel reported %d delivered, more than the %d sent channel=%s; treating all as delivered",
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf("[notify] failed to mark as delivered channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// This message hit the channel length cap: requeue the rest immediately for the next tick.
			// Use DeferDeliveries rather than RescheduleDeliveries — this is not a failure
			// and must not consume retry budget (the claim optimistically added 1; it is subtracted there).
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf(trMsg(errDeliveryChannelLengthCapped), delivered)); err != nil {
				log.Printf("[notify] failed to queue remaining segments channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// The channel neither reported an error nor said how many items were delivered. Treat it as a failure (with backoff)
		// so this delivery is not re-claimed forever without ever being marked.
		err = fmt.Errorf(trMsg(errDeliveryNoDeliveredCount), delivered)
	}

	// Failure handling is decided **per item**, not from the batch's maximum attempt count.
	//
	// It used to be `if maxAttempts(deliveries) >= MaxNotifyAttempts` failing the whole batch, but items in a batch
	// have different attempt counts: an old delivery already retried twice (attempts=2) would drag brand-new
	// deliveries (attempts=1) in the same batch into failed — new vulnerabilities lost permanently without a single retry,
	// the exact opposite of the intent "do not let old rows sink new ones".
	permanent := notify.IsPermanent(err)
	var failIDs, exhaustedIDs []int64
	byDelay := map[time.Duration][]int64{}
	for _, dl := range deliveries {
		switch {
		case permanent:
			failIDs = append(failIDs, dl.ID)
		case dl.Attempts >= db.MaxNotifyAttempts:
			exhaustedIDs = append(exhaustedIDs, dl.ID)
		default:
			delay := notifyBackoff[min(dl.Attempts, len(notifyBackoff)-1)]
			byDelay[delay] = append(byDelay[delay], dl.ID)
		}
	}

	if len(failIDs) > 0 {
		if fErr := n.pg.FailDeliveries(ctx, failIDs, err.Error()); fErr != nil {
			log.Printf("[notify] failed to mark failure state channel=%s ids=%v: %v", channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf(trMsg(errDeliveryRetryExhausted), db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf("[notify] failed to mark failure state channel=%s ids=%v: %v", channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Regroup by delay: there are only 3 backoff levels, so the number of groups is naturally small; no need to issue one
	// UPDATE per item (that would make a 500-item batch cost 500 round trips).
	for delay, group := range byDelay {
		if rErr := n.pg.RescheduleDeliveries(ctx, group, delay, err.Error()); rErr != nil {
			log.Printf("[notify] failed to requeue deliveries channel=%s ids=%v: %v", channel.Kind(), group, rErr)
		}
	}
	if len(failIDs)+len(exhaustedIDs) > 0 {
		log.Printf("[notify] delivery failed channel=%d kind=%s permanent=%d retries_exhausted=%d pending_retry=%d: %s",
			deliveries[0].ChannelID, channel.Kind(), len(failIDs), len(exhaustedIDs), len(byDelay), err)
	}
}

// excludeDeliveries returns those in all that are not in keep (compared by pointer identity).
// Used to find deliveries that "did not make it into the message" — they must be handled explicitly, not left in a gray zone.
func excludeDeliveries(all, keep []*db.NotificationDelivery) []*db.NotificationDelivery {
	inKeep := make(map[*db.NotificationDelivery]bool, len(keep))
	for _, dl := range keep {
		inKeep[dl] = true
	}
	var out []*db.NotificationDelivery
	for _, dl := range all {
		if !inKeep[dl] {
			out = append(out, dl)
		}
	}
	return out
}

// adapt gets the channel implementation and parses its config.
// ok=false means the kind is not registered; the delivery should fail outright rather than retry forever.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// On config parse failure, pass an empty map: the channel's own Validate reports "which field is missing",
		// which guides the user to a fix better than a JSON parse error.
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return channel, cfg, true
}

// renderSingle renders a single-vulnerability message.
func (n *Notifier) renderSingle(ctx context.Context, dl *db.NotificationDelivery, baseURL string) (notify.Message, error) {
	snap, err := parseSnapshot(dl)
	if err != nil {
		return notify.Message{}, err
	}
	item, err := n.itemFor(ctx, snap, baseURL)
	if err != nil {
		return notify.Message{}, err
	}
	return notify.Message{Items: []notify.Item{item}, HomeURL: baseURL}, nil
}

// renderBatch renders a digest message. Snapshots are parsed one by one — a single broken one is skipped
// without dragging down the whole digest.
//
// The returned included corresponds **strictly one-to-one** with msg.Items (i-th delivery ↔ i-th item).
// This is a hard requirement: the caller marks the first K deliveries delivered based on "the channel reported fitting the first K".
// If a broken snapshot were skipped here without removing that delivery from included,
// indexes would shift — a broken item that should fail would be marked delivered, and a good one judged undelivered.
// The broken ones are explicitly failed by the caller; see stepDigest.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// A broken snapshot goes into neither the message nor included — the caller handles it
			// (explicitly marks it failed rather than slipping it through as "delivered").
			log.Printf("[notify] skipped unparsable snapshot in digest batch delivery=%d: %v", dl.ID, err)
			continue
		}
		item, err := n.itemFor(ctx, snap, baseURL)
		if err != nil {
			return notify.Message{}, nil, err
		}
		items = append(items, item)
		included = append(included, dl)
	}
	if len(items) == 0 {
		return notify.Message{}, nil, fmt.Errorf(trMsg(errDeliveryBatchAllUnparseable), len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor renders an event snapshot into a push item, resolving the asset name and detail back-link along the way.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// Failing to resolve the asset name must not block the push: a missing name is far less bad than a missing notification;
		// the message just lacks the asset line.
		log.Printf("[notify] failed to resolve asset name finding=%d: %v", snap.FindingID, err)
	}
	item := notify.Item{
		FindingID:  snap.FindingID,
		Name:       snap.Name,
		VulnClass:  snap.VulnClass,
		Severity:   snap.Severity,
		Summary:    snap.Summary,
		Assets:     assets,
		FromStatus: snap.FromStatus,
		ToStatus:   snap.ToStatus,
	}
	if baseURL != "" {
		// The detail page route is web/src/app/(main)/function/findings/detail/page.tsx,
		// which reads the vulnerability id from the id query parameter.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens takes **at most want** tokens from the channel bucket and returns how many it actually got.
//
// One token = one message (one HTTP request). In realtime mode the caller passes as many as it needs;
// in digest mode a whole batch sends one message, so it passes 1.
//
// The bucket capacity is the channel's per-minute limit, refilled at a constant rate. ratePerMin<=0 means no rate limit,
// returning a finite but large enough value so a single round is not held up by an unbounded backlog.
//
// The want cap is required: without it the bucket could only be drained entirely, while the caller has its own per-round cap;
// the extra tokens would go unused and vanish before the next refill — the accumulated burst capacity would never be reachable,
// and even "no pending deliveries this round" would still be charged.
func (n *Notifier) takeTokens(channelID int64, ratePerMin, want int, now time.Time) int {
	if want <= 0 {
		return 0
	}
	if ratePerMin <= 0 {
		return min(want, notifyUnlimitedBurstPerTick)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	b := n.buckets[channelID]
	if b == nil {
		b = &notifyBucket{tokens: float64(ratePerMin), lastFill: now}
		n.buckets[channelID] = b
	}
	// Refill by real elapsed time at ratePerMin/60 per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before truncating: the token count is a float accumulation, and filling in two steps
	// 0.5 + 0.5 may give 0.9999999999, which int() truncates to 0 —
	// a mathematically full bucket yielding no token. 1e-9 is far below one token and never lets a real deficit through.
	take := min(int(b.tokens+1e-9), want)
	if take <= 0 {
		return 0
	}
	b.tokens -= float64(take)
	return take
}

// enabled reads the master switch.
func (n *Notifier) enabled() bool {
	return n.pg.GetBool(settingNotifyEnabled, true)
}

// publicBaseURL returns the external base URL for back-links, without a trailing slash.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval returns the digest period, falling back to the default when invalid or unset.
func (n *Notifier) digestInterval() time.Duration {
	v, ok, err := n.pg.GetSetting(settingNotifyDigestMinutes)
	if err != nil || !ok {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	m := 0
	if _, err := fmt.Sscanf(v, "%d", &m); err != nil || m <= 0 {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// parseSnapshot parses the snapshot of the event behind a delivery.
func parseSnapshot(dl *db.NotificationDelivery) (notify.Snapshot, error) {
	var snap notify.Snapshot
	if len(dl.Snapshot) == 0 {
		return snap, fmt.Errorf(trMsg(errDeliverySnapshotEmpty), dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, fmt.Errorf(trMsg(errDeliverySnapshotParse), dl.ID, err)
	}
	if snap.Kind == "" {
		// The event type comes from the event row; the copy in the snapshot may have been written by an older version.
		snap.Kind = dl.EventKind
	}
	return snap, nil
}

func deliveryIDs(deliveries []*db.NotificationDelivery) []int64 {
	out := make([]int64, 0, len(deliveries))
	for _, dl := range deliveries {
		out = append(out, dl.ID)
	}
	return out
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
