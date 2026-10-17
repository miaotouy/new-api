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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'

import { channelSchema } from '../../types'
import { ChannelsProvider, useChannels } from '../channels-provider'
import { ChannelTestDialog } from '../dialogs/channel-test-dialog'

function TestChannel() {
  const { setCurrentRow } = useChannels()
  return (
    <>
      <Button
        onClick={() =>
          setCurrentRow(
            channelSchema.parse({
              id: 1,
              type: 1,
              key: '',
              name: 'Example channel',
              status: 1,
              created_time: 0,
              test_time: 0,
              response_time: 0,
              balance_updated_time: 0,
              models: 'example-model',
            })
          )
        }
      >
        Open channel test
      </Button>
      <ChannelTestDialog open onOpenChange={() => undefined} />
    </>
  )
}

// Protect the test -> response -> copy flow for JSON, streams, and failures.
test.each([
  {
    name: 'JSON',
    success: true,
    body: '{"choices":[{"message":{"content":"你好"}}]}',
    displayedBody: `{
  "choices": [
    {
      "message": {
        "content": "你好"
      }
    }
  ]
}`,
    truncated: false,
  },
  {
    name: 'SSE preview',
    success: true,
    body: 'data: {"choices":[{"delta":{"content":"你好"}}]}\n\ndata: [DONE]\n\n',
    displayedBody:
      'data: {"choices":[{"delta":{"content":"你好"}}]}\n\ndata: [DONE]\n\n',
    truncated: true,
  },
  {
    name: 'failed response',
    success: false,
    body: '{"error":{"message":"upstream failed"}}',
    displayedBody: `{
  "error": {
    "message": "upstream failed"
  }
}`,
    truncated: false,
  },
])(
  'shows and copies the $name returned by the model',
  async ({ success, body, displayedBody, truncated }) => {
    const user = userEvent.setup()
    const clipboard = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockResolvedValue()
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success,
        message: success ? '' : 'upstream failed',
        time: 1.25,
        response_body: body,
        response_truncated: truncated,
      },
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <ChannelsProvider>
          <TestChannel />
        </ChannelsProvider>
      </QueryClientProvider>
    )
    await user.click(screen.getByRole('button', { name: 'Open channel test' }))
    await user.click(screen.getByRole('button', { name: 'Test Connection' }))
    await user.click(
      await screen.findByRole('button', { name: 'View response' })
    )
    const dialog = await screen.findByRole('dialog', { name: 'Model response' })
    expect(
      within(dialog).getByText(
        (_, element) =>
          element?.tagName === 'PRE' && element.textContent === displayedBody
      )
    ).toBeVisible()
    if (truncated) {
      expect(within(dialog).getByRole('status')).toBeVisible()
    }
    await user.click(
      within(dialog).getByRole('button', { name: 'Copy to clipboard' })
    )
    await waitFor(() => expect(clipboard).toHaveBeenCalledWith(displayedBody))
    view.unmount()
    client.clear()
  }
)
