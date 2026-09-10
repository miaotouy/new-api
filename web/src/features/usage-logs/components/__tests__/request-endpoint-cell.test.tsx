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
import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { RequestEndpointCell } from '../request-endpoint-cell'

describe('usage log request endpoint cell', () => {
  test('renders one path without direction labels when the endpoints match', () => {
    render(
      <RequestEndpointCell
        other={{
          request_path: '/v1/responses',
          upstream_request_path: '/v1/responses',
        }}
      />
    )

    expect(screen.getByTitle('/v1/responses')).toBeInTheDocument()
    expect(screen.queryByTitle(/^Inbound:/)).toBeNull()
    expect(screen.queryByTitle(/^Outbound:/)).toBeNull()
  })

  test('renders inbound and outbound lines when the endpoint is converted', () => {
    render(
      <RequestEndpointCell
        other={{
          request_path: '/v1/chat/completions',
          upstream_request_path: '/v1/responses',
        }}
      />
    )

    expect(
      screen.getByTitle('Inbound: /v1/chat/completions')
    ).toBeInTheDocument()
    expect(screen.getByTitle('Outbound: /v1/responses')).toBeInTheDocument()
  })

  test('renders one path when the upstream endpoint is unknown', () => {
    render(
      <RequestEndpointCell other={{ request_path: '/v1/chat/completions' }} />
    )

    expect(screen.getByTitle('/v1/chat/completions')).toBeInTheDocument()
    expect(screen.queryByTitle(/^Outbound:/)).toBeNull()
  })

  test('renders nothing when the log has no request path', () => {
    const { container } = render(<RequestEndpointCell other={null} />)

    expect(container).toBeEmptyDOMElement()
  })
})
