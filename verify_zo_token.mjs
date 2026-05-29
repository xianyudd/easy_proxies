#!/usr/bin/env node

const API_URL = 'https://api.zo.computer/zo/ask';

const token = process.env.ZO_API_TOKEN;
if (!token) {
  console.error('❌ Missing ZO_API_TOKEN environment variable.');
  console.error('Please set it first, for example:');
  console.error('  export ZO_API_TOKEN="zo_sk_xxx"');
  process.exit(1);
}

async function readResponseBody(response) {
  const text = await response.text();
  if (!text) return { text: '', json: null };

  try {
    return { text, json: JSON.parse(text) };
  } catch (error) {
    throw new Error(`Failed to parse JSON response: ${error.message}\nResponse body: ${text}`);
  }
}

async function main() {
  let response;

  try {
    response = await fetch(API_URL, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        input: 'Please reply with only: OK',
        stream: false,
      }),
    });
  } catch (error) {
    console.error('❌ Network error while calling Zo API');
    console.error(error.message);
    process.exit(1);
  }

  let body;
  try {
    body = await readResponseBody(response);
  } catch (error) {
    console.error('❌ Failed to parse Zo API response JSON');
    console.error(error.message);
    process.exit(1);
  }

  if (response.status === 200 && body.json && Object.prototype.hasOwnProperty.call(body.json, 'output')) {
    console.log('✅ Zo API key is valid');
    console.log(`output: ${body.json.output}`);
    if (body.json.conversation_id) {
      console.log(`conversation_id: ${body.json.conversation_id}`);
    }
    return;
  }

  if (response.status === 401 || response.status === 403) {
    console.error('❌ Zo API key is invalid or unauthorized');
    console.error(`status: ${response.status}`);
    console.error(`response body: ${body.text}`);
    process.exit(1);
  }

  if (!response.ok) {
    console.error('❌ Zo API request failed');
    console.error(`status: ${response.status}`);
    console.error(`response body: ${body.text}`);
    process.exit(1);
  }

  console.error('❌ Zo API response did not contain an output field');
  console.error(`status: ${response.status}`);
  console.error(`response body: ${body.text}`);
  process.exit(1);
}

main();
