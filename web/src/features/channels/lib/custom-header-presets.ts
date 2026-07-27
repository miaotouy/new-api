/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export type CustomHeaderPreset = {
  id: string
  labelKey: string
  headers: Readonly<Record<string, string>>
}

export const CUSTOM_HEADER_PRESETS = [
  {
    id: 'aio-hub-default',
    labelKey: 'AIO Hub Default',
    headers: {
      'User-Agent':
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
      'X-App-Name': 'AIO Hub',
      'X-App-Version': '1.0.0',
      'x-title': 'AIO Hub',
      'HTTP-Referer': 'https://aiohub-app.com',
      Origin: 'https://aiohub-app.com',
      'X-OpenRouter-Title': 'AIO Hub',
      'sec-ch-ua':
        '"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"',
      'sec-ch-ua-mobile': '?0',
      'sec-ch-ua-platform': '"Windows"',
      'sec-fetch-site': 'cross-site',
      'sec-fetch-mode': 'cors',
      'sec-fetch-dest': 'empty',
      'accept-language': 'zh-CN,zh;q=0.9,en;q=0.8',
    },
  },
  {
    id: 'browser-client',
    labelKey: 'Browser Client',
    headers: {
      'User-Agent':
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
      'sec-ch-ua':
        '"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"',
      'sec-ch-ua-mobile': '?0',
      'sec-ch-ua-platform': '"Windows"',
      'sec-fetch-site': 'cross-site',
      'sec-fetch-mode': 'cors',
      'sec-fetch-dest': 'empty',
      'accept-language': 'zh-CN,zh;q=0.9,en;q=0.8',
      'x-title': 'AIO Hub',
    },
  },
  {
    id: 'codex-cli',
    labelKey: 'Codex CLI',
    headers: {
      'User-Agent':
        'codex_cli_rs/0.114.0 (Mac OS 14.2.0; x86_64) vscode/1.111.0',
      Accept: 'application/json',
    },
  },
  {
    id: 'timeout-headers',
    labelKey: 'Timeout Headers',
    headers: {
      'x-stainless-timeout': '600',
      'x-stainless-retry-count': '0',
    },
  },
  {
    id: 'source-identification',
    labelKey: 'Source Identification',
    headers: {
      'HTTP-Referer': 'https://aiohub-app.com',
      origin: 'https://aiohub-app.com',
    },
  },
  {
    id: 'claude-code',
    labelKey: 'Claude Code',
    headers: {
      Accept: 'application/json',
      'X-Stainless-Retry-Count': '0',
      'X-Stainless-Timeout': '600',
      'X-Stainless-Lang': 'js',
      'X-Stainless-Package-Version': '0.74.0',
      'X-Stainless-OS': 'Windows',
      'X-Stainless-Arch': 'x64',
      'X-Stainless-Runtime': 'node',
      'X-Stainless-Runtime-Version': 'v24.11.1',
      'anthropic-dangerous-direct-browser-access': 'true',
      'x-app': 'cli',
      'User-Agent': 'claude-cli/2.1.144 (external, cli)',
      'anthropic-beta':
        'claude-code-20250219,context-1m-2025-08-07,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14',
      'accept-language': '*',
      'sec-fetch-mode': 'cors',
    },
  },
  {
    id: 'openrouter-attribution',
    labelKey: 'OpenRouter Attribution',
    headers: {
      'HTTP-Referer': 'https://aiohub-app.com',
      'X-OpenRouter-Title': 'AIO Hub',
      'X-OpenRouter-Categories': 'general-chat',
    },
  },
] as const satisfies readonly CustomHeaderPreset[]
