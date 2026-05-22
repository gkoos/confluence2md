## Summary

The service currently has no deduplication enforcement at any layer.

---

### Option 1

Allow retries before first success.

| Pros | Cons |
| --- | --- |
| Simple rule | Race window |

```js
const a = 1;
```

Ticket [FLS1-20](/browse/FLS1-20)