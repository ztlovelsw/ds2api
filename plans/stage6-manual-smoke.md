# Stage 6 Manual Smoke Checklist

- Date:
- Tester:
- Environment:

## Items

1. Login flow (`/admin/login`) succeeds and failure message shape unchanged.
2. Account manager:
   - add/edit/delete account
   - queue status cards render and refresh
3. API tester:
   - non-stream request succeeds
   - stream request receives incremental output and final state
4. Settings:
   - read settings
   - save settings
   - backup/export path works
5. Vercel sync:
   - status poll
   - manual refresh
   - sync action and status feedback text

## Result

- Status: `PENDING`
- Notes:

