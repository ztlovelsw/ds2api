#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const chatStream = require('../../api/chat-stream.js');
const { parseChunkForContent } = chatStream.__test;

function parseArgs(argv) {
  const out = {
    samplesRoot: 'tests/raw_stream_samples',
    reportPath: '',
    failOnLeak: true,
    failOnMissingFinish: true,
  };
  for (let i = 2; i < argv.length; i += 1) {
    const a = argv[i];
    if (a === '--samples-root' && argv[i + 1]) {
      out.samplesRoot = argv[++i];
    } else if (a === '--report' && argv[i + 1]) {
      out.reportPath = argv[++i];
    } else if (a === '--no-fail-on-leak') {
      out.failOnLeak = false;
    } else if (a === '--no-fail-on-missing-finish') {
      out.failOnMissingFinish = false;
    }
  }
  return out;
}

function findSampleDirs(root) {
  if (!fs.existsSync(root)) {
    return [];
  }
  return fs.readdirSync(root)
    .map((name) => path.join(root, name))
    .filter((p) => fs.statSync(p).isDirectory())
    .filter((p) => fs.existsSync(path.join(p, 'upstream.stream.sse')))
    .sort();
}

function parseSSE(raw) {
  const events = [];
  for (const block of raw.split(/\r?\n\r?\n/)) {
    if (!block.trim()) {
      continue;
    }
    let eventType = 'message';
    const dataLines = [];
    for (const line of block.split(/\r?\n/)) {
      if (line.startsWith('event:')) {
        eventType = line.slice(6).trim() || 'message';
      } else if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trimStart());
      }
    }
    if (dataLines.length === 0) {
      continue;
    }
    const payload = dataLines.join('\n').trim();
    events.push({ event: eventType, payload });
  }
  return events;
}

function replaySample(raw) {
  const events = parseSSE(raw);
  let currentType = 'thinking';
  let sawFinish = false;
  let outputText = '';
  let parsedChunks = 0;

  for (const evt of events) {
    if (evt.event === 'finish') {
      sawFinish = true;
    }
    if (!evt.payload || evt.payload === '[DONE]' || evt.payload[0] !== '{') {
      continue;
    }
    let obj;
    try {
      obj = JSON.parse(evt.payload);
    } catch {
      continue;
    }
    parsedChunks += 1;
    const parsed = parseChunkForContent(obj, true, currentType);
    currentType = parsed.newType;
    if (parsed.finished) {
      sawFinish = true;
    }
    for (const part of parsed.parts) {
      outputText += part.text;
    }
  }

  return {
    events: events.length,
    parsedChunks,
    sawFinish,
    leakedFinishedText: outputText.includes('FINISHED'),
    outputChars: outputText.length,
  };
}

function main() {
  const opts = parseArgs(process.argv);
  const dirs = findSampleDirs(opts.samplesRoot);
  if (dirs.length === 0) {
    console.error(`[sim] no samples found: ${opts.samplesRoot}`);
    process.exit(1);
  }

  const report = {
    generated_at: new Date().toISOString(),
    samples_root: opts.samplesRoot,
    total: dirs.length,
    failed: 0,
    samples: [],
  };

  for (const dir of dirs) {
    const sampleID = path.basename(dir);
    const raw = fs.readFileSync(path.join(dir, 'upstream.stream.sse'), 'utf8');
    const r = replaySample(raw);
    const errors = [];
    if (opts.failOnMissingFinish && !r.sawFinish) {
      errors.push('missing finish signal');
    }
    if (opts.failOnLeak && r.leakedFinishedText) {
      errors.push('FINISHED leaked into output text');
    }
    if (errors.length > 0) {
      report.failed += 1;
    }
    report.samples.push({ sample_id: sampleID, ...r, ok: errors.length === 0, errors });
  }

  if (opts.reportPath) {
    fs.writeFileSync(opts.reportPath, JSON.stringify(report, null, 2));
  }

  for (const s of report.samples) {
    const status = s.ok ? 'OK' : 'FAIL';
    const note = s.errors.length > 0 ? ` errors=${s.errors.join(';')}` : '';
    console.log(`[sim] ${status} ${s.sample_id} events=${s.events} parsed=${s.parsedChunks} chars=${s.outputChars}${note}`);
  }

  if (report.failed > 0) {
    console.error(`[sim] ${report.failed}/${report.total} samples failed`);
    process.exit(2);
  }
  console.log(`[sim] all ${report.total} samples passed`);
}

main();
