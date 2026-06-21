<!-- This prompt will be imported in the agentic workflow .github/workflows/large-payload-tester.md at runtime. -->
<!-- You can edit this file to modify the agent behavior without recompiling the workflow. -->

# Large MCP Payload Access Test

You are an AI agent testing the MCP Gateway's large payload mechanism.

## Your Task

1. Use the **`filesystem-read_file` MCP tool** to read `/workspace/large-test-file.json`.
2. The file is ~500KB so the gateway will return a truncated response containing a `payloadPath` field.
3. Use bash to read the full JSON from `payloadPath` (e.g. `cat <payloadPath>`).
4. Extract the top-level `secret_reference` field from that JSON.
5. Use the **`filesystem-read_file` MCP tool** to read `/workspace/secret.txt`.
6. Verify the two secrets match.

**Do NOT use bash to read files directly from `/tmp/mcp-test-fs/`. You must use the `filesystem-read_file` tool so the large payload mechanism is exercised.**

## Tool Usage

The filesystem MCP server exposes these tools (prefixed with `filesystem-`):
- **`filesystem-read_file`** — reads a file; use this for both test files
- `filesystem-list_directory` — lists directory contents (use `/workspace` as the path)

Files available in the MCP server at `/workspace/`:
- `/workspace/large-test-file.json` — ~500KB JSON file, triggers the large payload path
- `/workspace/secret.txt` — contains the expected secret value

## Understanding the Large Payload Response

When a file exceeds the payload size threshold, the `filesystem-read_file` response is a JSON object with:
- `payloadPath`: Absolute path to the full response data on the local filesystem
- `payloadPreview`: First 500 characters of the response (preview only)
- `payloadSchema`: Structure/type info — does NOT contain actual values
- `originalSize`: Size of the full response in bytes
- `agentInstructions`: Instructions for accessing the full data

**The `secret_reference` field is at the TOP LEVEL of the full JSON at `payloadPath`.** Read the file at `payloadPath` with bash and parse it as JSON to find `secret_reference`.

## Important Notes

- **Use `filesystem-read_file` tool** — do not use bash to read files in `/workspace` or `/tmp/mcp-test-fs/`
- **Keep all outputs concise** — use brief, factual statements
- **Log all key values** — secret, paths, sizes
- **Be explicit about failures** — state exactly what went wrong if any step fails

## Expected Behavior

**Success scenario:**
1. Agent calls `filesystem-read_file` with path `/workspace/large-test-file.json`.
2. Gateway returns truncated response with `payloadPath` (e.g. `/tmp/gh-aw/mcp-payloads/...`).
3. Agent reads the full JSON from `payloadPath` using bash: `cat <payloadPath> | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['secret_reference'])"`.
4. Agent calls `filesystem-read_file` with path `/workspace/secret.txt`.
5. Agent compares the two secrets — they must match.

**Failure scenarios to detect:**
- `filesystem-read_file` does not return a `payloadPath` (large payload mechanism not triggered)
- Agent can't read the payload file at `payloadPath` (permission/mount issues)
- `secret_reference` is missing from the full JSON (data integrity issue)
- Secrets do not match

## Output Format

After running all tests, create an issue with:
- Title: "Large Payload Test - ${{ github.run_id }}"
- Body with test results in this format:

```markdown
# Large MCP Payload Access Test Results

**Run ID:** ${{ github.run_id }}
**Status:** [PASS/FAIL]
**Timestamp:** [current time]

## Test Results

- **Expected Secret:** [UUID from test-secret.txt]
- **Found Secret:** [UUID from payload] or "NOT FOUND"
- **Secret Match:** [YES/NO]
- **Payload Path:** [path from response]
- **Payload Size:** [originalSize from metadata]

## Conclusion

[Brief summary of what worked and what failed, if anything]

---
Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
```
