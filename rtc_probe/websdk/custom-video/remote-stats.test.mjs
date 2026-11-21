import { test } from 'node:test';
import assert from 'node:assert/strict';
import { remoteStatsSample, evaluateRemoteStats } from './remote-stats.mjs';

const audio = (bytes, packets) => ({ receiveBytes: bytes, receivePackets: packets, receiveBitrate: 32000 });
const video = (bytes, packets) => ({ receiveBytes: bytes, receivePackets: packets, receiveBitrate: 120000, receiveResolutionWidth: 320, receiveResolutionHeight: 180, receiveFrameRate: 12, decodeFrameRate: 12, renderFrameRate: 12 });
const sample = (a, v, uid = '10001') => ({ uid, audio: a, video: v });
const healthy = () => [sample(audio(100, 10), video(1000, 2)), sample(audio(300, 30), video(4000, 8))];

test('requires remote SDK counter progress and decoded/rendered video for the expected UID', () => {
  const result = evaluateRemoteStats(healthy(), '10001');
  assert.equal(result.accepted, true);
  assert.equal(result.audio.received_bytes_delta, 200);
  assert.equal(result.video.received_packets_delta, 6);
});
test('unrelated publisher statistics cannot satisfy the probe', () => {
  const state = { session: { remoteAudioStats: { 10002: audio(100, 10) }, remoteVideoStats: { 10002: video(1000, 2) } } };
  const samples = [remoteStatsSample(state, '10001', 0), remoteStatsSample(state, '10001', 1000)];
  assert.equal(evaluateRemoteStats(samples, '10001').reason, 'WEB_SDK_REMOTE_AUDIO_STATS_MISSING');
});
test('cached nonzero counters and a single sample do not prove receiving', () => {
  const s = healthy()[1];
  assert.equal(evaluateRemoteStats([s, s], '10001').accepted, false);
  assert.equal(evaluateRemoteStats([s], '10001').accepted, false);
});
test('missing counters are unavailable rather than zero', () => {
  const samples = healthy(); delete samples[1].audio.receivePackets;
  const result = evaluateRemoteStats(samples, '10001');
  assert.equal(result.accepted, false);
  assert.equal(result.audio.received_packets_delta, null);
});
test('network video traffic alone cannot prove video decoding', () => {
  for (const field of ['decodeFrameRate', 'renderFrameRate', 'receiveResolutionWidth']) {
    const samples = healthy(); samples[1].video[field] = 0;
    assert.equal(evaluateRemoteStats(samples, '10001').reason, 'WEB_SDK_REMOTE_VIDEO_NOT_DECODING');
  }
});
test('a missing final video snapshot cannot reuse previous successful statistics', () => {
  const samples = healthy(); samples.push(sample(audio(400, 40), null));
  assert.equal(evaluateRemoteStats(samples, '10001').reason, 'WEB_SDK_REMOTE_VIDEO_STATS_MISSING');
});
test('counter reset cannot be mistaken for progress and pre-media bytes are excluded', () => {
  const samples = healthy(); samples.push(sample(audio(50, 1), video(500, 1)));
  assert.equal(evaluateRemoteStats(samples, '10001').audio.counters_reset, true);
  assert.equal(evaluateRemoteStats(samples, '10001').accepted, false);
  const baseline = sample(audio(90, 9), video(900, 1));
  assert.equal(evaluateRemoteStats(healthy(), '10001', baseline).audio.received_bytes_delta, 210);
});
