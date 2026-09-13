# Captain MCP server

Captain exposes its admin operations to AI agents through the Model Context
Protocol: JSON-RPC 2.0 over HTTP at `POST /mcp` (streamable HTTP, no SSE
stream). Authenticate with a personal API token created in Settings → API
tokens & MCP; the token carries its owner's role.

## Connect

Claude Code:

```sh
claude mcp add --transport http captain https://your-panel/mcp \
  --header "Authorization: Bearer cap_..."
```

Any MCP client config:

```json
{ "mcpServers": { "captain": { "type": "http", "url": "https://your-panel/mcp",
  "headers": { "Authorization": "Bearer cap_..." } } } }
```

## Tools

| Tool | Role | What it does |
|---|---|---|
| `dashboard` | all staff | users, subscriptions, nodes online, revenue, pending orders, open tickets |
| `node_list`, `node_detail` | admin, operator | nodes, inbounds, live host metrics, probe data, monthly traffic |
| `plan_list` | all staff | plans and periods |
| `user_list`, `user_detail` | all staff | search users; one user with subscription, orders, devices |
| `order_list` | all staff | recent orders by status |
| `ticket_list`, `ticket_detail` | all staff | support tickets |
| `probe_snapshot` | admin, operator | live metrics of every node |
| `tcping` | admin, operator | TCP-connect latency from the panel to host:port |
| `external_node_list` | admin, operator | imported nodes and subscription sources |
| `user_create` ✱ | admin, operator | create a user |
| `user_grant_plan` ✱ | admin, operator | give a plan now |
| `user_adjust` ✱ | admin, operator | extend days, quota override, reset day, reset usage |
| `user_balance` ✱ | admin, operator | add or subtract balance |
| `user_set_status` ✱ | admin, operator | ban / re-activate |
| `ticket_reply` ✱ | all staff | reply as staff; the user is notified |

✱ write tools refuse to run unless the call carries `confirm: true`, so an
agent has to state the change before doing it. Destructive operations
(deleting users or nodes, rotating tokens, changing settings) are not exposed.
