import http from 'node:http';
import fs from 'node:fs';
import { promises as fsp } from 'node:fs';
import path from 'node:path';
import { pipeline } from 'node:stream';
import { fileURLToPath } from 'node:url';
const here = path.dirname(fileURLToPath(import.meta.url));

export async function startRuntimeServer({ mediaPath }) {
  const info = await fsp.stat(mediaPath);
  if (!info.isFile()) throw new Error('mediaPath must be a file');
  const html = await fsp.readFile(path.join(here, 'browser/runtime.html'), 'utf8');
  const files = {
    '/monitor.js': [path.join(here, 'browser/monitor.js'), 'text/javascript'],
    '/integration.js': [path.join(here, 'browser/integration.js'), 'text/javascript'],
    '/sdk.js': [path.join(here, 'sdk/AgoraRTC_N.js'), 'text/javascript'],
    '/media/input': [mediaPath, path.extname(mediaPath).toLowerCase() === '.webm' ? 'video/webm' : 'video/mp4'],
  };
  const server = http.createServer(async (request, response) => {
    try {
      const url = new URL(request.url, 'http://127.0.0.1');
      if (url.pathname === '/') {
        const body = html;
        response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
        response.end(body); return;
      }
      if (url.pathname === '/favicon.ico') { response.writeHead(204); response.end(); return; }
      const asset = files[url.pathname];
      if (!asset) { response.writeHead(404); response.end('Not found'); return; }
      const [filename, contentType] = asset;
      const stat = await fsp.stat(filename);
      let start = 0, end = stat.size - 1, status = 200;
      if (request.headers.range) {
        const match = /^bytes=(\d*)-(\d*)$/.exec(request.headers.range);
        if (!match || (!match[1] && !match[2])) { response.writeHead(416, { 'Content-Range': `bytes */${stat.size}` }); response.end(); return; }
        if (match[1]) { start = Number(match[1]); end = match[2] ? Math.min(Number(match[2]), end) : end; }
        else start = Math.max(0, stat.size - Number(match[2]));
        if (start > end || start >= stat.size) { response.writeHead(416, { 'Content-Range': `bytes */${stat.size}` }); response.end(); return; }
        status = 206;
      }
      const headers = { 'Content-Type': contentType, 'Content-Length': end - start + 1, 'Accept-Ranges': 'bytes', 'Cache-Control': 'no-store' };
      if (status === 206) headers['Content-Range'] = `bytes ${start}-${end}/${stat.size}`;
      response.writeHead(status, headers);
      if (request.method === 'HEAD') response.end();
      else pipeline(fs.createReadStream(filename, { start, end }), response, () => {});
    } catch { if (!response.headersSent) response.writeHead(500); response.end('Runtime asset error'); }
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
  return {
    url: `http://127.0.0.1:${server.address().port}/`,
    close: () => new Promise(resolve => { server.close(resolve); server.closeAllConnections(); }),
  };
}
