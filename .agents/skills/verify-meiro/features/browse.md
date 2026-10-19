# Browse pages

A user opens the pages in the rail, follows an item into an album, playlist or artist page, and goes back to where they were.

## Sub-features

- `browse-pages` draws every route with its seeded content.
- `browse-open` follows an item from Home into its page.
- `browse-back` restores the scroll position of the page left.
- `browse-new-route` covers a page added to `routes` in `routes.go`.

## How to get to it (user POV)

- Click `Home`, `Explore`, `Library` or `Settings` in the rail.
- Press `Cmd+1`, `Cmd+2`, `Cmd+3` or `Cmd+,`.
- Click an album, playlist or artist card on any page.
- Open a `meiro://` link (see [native-only.md](./native-only.md)).

## Driving it with the headless tester

Preconditions:

- `scripts/doctor.sh` passes.
- Start from `a := newTestApp(t)`, `a.router.Push("/home")`, `tt := ui.NewTester(a.view, 1000, 700)`.

- **Every page.** Push each path in `TestEveryPageLoadsAndDraws` and call `p.step`. `Home` shows `Quick picks` and `Ambient One`. `/explore` shows `Moods` and `Night Drive`. `/album/MPREb_test` shows `Album Track One`.
- **Open an item.** From `/home`, `p.click("Deep Focus", "album")`. `a.router.Path()` starts with `/album/` and `Album Track One` shows.
- **Show more.** On Home, click `Show more`. The next sections appear below the existing feed, and the request count increases once.
- **Go back.** Verified for album: click `Deep Focus` on `/home`, then `a.router.Back()` and `p.step("back")`. `Quick picks` shows again. For scroll position, mirror `TestGoingBackRestoresWhereAPageWasScrolled`, which uses `/settings`, `tt.Scroll(500, 300, 0, 1500)` and `a.list.Visible()`.
- **New route.** Add the route's path to the table in `TestEveryPageLoadsAndDraws` style, push it, and show its heading in the screenshot.

## Gotchas

- `/library` and `/recap` need `signedInApp(t)`. Signed out they show a sign-in prompt, which is also worth a screenshot.
- Compare the heading, not the path. A route that failed to match draws the `Not found` page at a valid-looking path.
- `a.router.Push` is the test's shortcut for navigation. Also click one real entry point per proof.
