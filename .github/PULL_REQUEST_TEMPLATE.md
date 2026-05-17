## Summary

- 

## Test plan

- [ ] `go test ./...`
- [ ] `./build.sh`
- [ ] Live smoke test if this changes controller, proxy, browser, process, or state behavior

## Safety checklist

- [ ] Does not commit generated runtime configs, state files, logs, local config, or binaries
- [ ] Does not include secrets, subscription URLs, proxy credentials, private node names, or local absolute paths
- [ ] Keeps read-only commands read-only against the main Clash Verge/mihomo controller
- [ ] Keeps temporary proxy/controller/DevTools endpoints bound to localhost
