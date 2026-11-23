// Keep SDK statistics scoped to the expected publisher. Missing values are not zero.
export function remoteStatsSample(state, uid, elapsedMS) {
  const session = state?.session || {}, key = String(uid);
  return {
    elapsed_ms: elapsedMS,
    uid: key,
    audio: session.remoteAudioStats?.[key] ?? null,
    video: session.remoteVideoStats?.[key] ?? null,
  };
}

const valid = value => typeof value === 'number' && Number.isFinite(value) && value >= 0;
function trackWindow(samples, kind, baseline) {
  const values = samples.map(sample => sample[kind]).filter(Boolean);
  const counters = values.filter(value => valid(value.receiveBytes) && valid(value.receivePackets));
  const first = baseline?.[kind] || counters[0], last = samples.at(-1)?.[kind] || null;
  const delta = key => first && last && valid(first[key]) && valid(last[key])
    ? last[key] - first[key] : null;
  let reset = false;
  if (baseline?.[kind] && counters.length &&
    (counters[0].receiveBytes < baseline[kind].receiveBytes || counters[0].receivePackets < baseline[kind].receivePackets)) reset = true;
  for (let i = 1; i < counters.length; i++) {
    if (counters[i].receiveBytes < counters[i - 1].receiveBytes || counters[i].receivePackets < counters[i - 1].receivePackets) reset = true;
  }
  const bytes = delta('receiveBytes'), packets = delta('receivePackets');
  const progress = counters.length >= 2 && !reset && bytes > 0 && packets > 0;
  return { last, progress, reset, received_bytes_delta: bytes, received_packets_delta: packets };
}

export function evaluateRemoteStats(samples, uid, baseline) {
  const scoped = samples.filter(sample => sample.uid === String(uid));
  const audio = trackWindow(scoped, 'audio', baseline), video = trackWindow(scoped, 'video', baseline);
  const audioRate = audio.last && valid(audio.last.receiveBitrate) && audio.last.receiveBitrate > 0;
  const videoDecode = ((value) => value &&
    valid(value.receiveBitrate) && value.receiveBitrate > 0 &&
    valid(value.receiveResolutionWidth) && value.receiveResolutionWidth > 0 &&
    valid(value.receiveResolutionHeight) && value.receiveResolutionHeight > 0 &&
    valid(value.receiveFrameRate) && value.receiveFrameRate > 0 &&
    valid(value.decodeFrameRate) && value.decodeFrameRate > 0 &&
    valid(value.renderFrameRate) && value.renderFrameRate > 0)(video.last);
  const audioOK = !!(audio.progress && audioRate), videoOK = !!(video.progress && videoDecode);
  const reason = !audio.last ? 'WEB_SDK_REMOTE_AUDIO_STATS_MISSING'
    : !audioOK ? 'WEB_SDK_REMOTE_AUDIO_NO_PROGRESS'
    : !video.last ? 'WEB_SDK_REMOTE_VIDEO_STATS_MISSING'
    : !video.progress ? 'WEB_SDK_REMOTE_VIDEO_NO_PROGRESS'
    : !videoDecode ? 'WEB_SDK_REMOTE_VIDEO_NOT_DECODING' : 'SDK_REMOTE_MEDIA_VERIFIED';
  const summary = value => ({ stats: value.last, received_bytes_delta: value.received_bytes_delta, received_packets_delta: value.received_packets_delta, counters_reset: value.reset });
  return { uid: String(uid), source: 'sdk_remote_stats', sample_count: scoped.length, audio_ok: audioOK, video_ok: videoOK, accepted: audioOK && videoOK, reason, audio: summary(audio), video: summary(video) };
}
