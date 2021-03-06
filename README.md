# podfeeds

This is an HTTP server-based podcast aggregator. Just as other aggregators use Chrome to present a GUI, this one uses Lynx
to present a TUI.

## Setting Up Lynx

Set Lynx up to open media files in your favorite media player. For example, I have the following ~/.mailcap to get Lynx to
open them in mpv:

    audio/*; mpv %s
    video/*; mpv %s
    application/vnd.rn-realmedia; mpv %s

Setting Lynx to always accept cookies will also save a few manual steps. Set the following (make sure they're not commented out) in lynx.cfg:

    FORCE_SSL_COOKIES_SECURE:TRUE
    SET_COOKIES:TRUE
    ACCEPT_ALL_COOKIES:TRUE

I like to set Lynx to use BASH-like keybindings (in the options menu you get with "o"). If a download URL has GET
parameters, it makes it easier to delete them from the filename (press Ctrl-A to go to the beginning of the
line, cursor to the question mark, Ctrl+K to delete to the end of the line).

Remember that Lynx caches everything. If you want to check for updates, you need to refresh manually
(Ctrl+R, or by following links with "x").

## Gathering Feed URLs

Gather your podcast feed URLs. One way to find them is to
use the "podfeed" script I posted here:

[CLI app to get podcast feed URLs
](https://www.linuxquestions.org/questions/linuxquestions-org-member-success-stories-23/cli-app-to-get-podcast-feed-urls-4175656322/#post6363987)

Paste them into a file in the root directory (the one with files like go.mod), named "podcasts.yaml". Separate each line with a
hyphen and a space, and use # for comments. For example:

    - https://www.cbc.ca/podcasting/includes/wr.xml # CBC World Report
    - https://www.cbc.ca/podcasting/includes/frontburner.xml
    - https://www.cbc.ca/podcasting/includes/current.xml
    # CBC AS It Happens
    - https://www.cbc.ca/podcasting/includes/asithappens.xml
    - https://www.cbc.ca/podcasting/includes/cbcpowerandpolitics.xml

## Building Podfeeds

    go build

## Running Podfeeds

    ./podfeeds

By default, the server will listen on port 8080. Set the PORT environment variable to override it. Set it to port 0 to let it pick
the port.

Load the server's index page in Lynx:

    lynx http://localhost:8080

## Using Podfeeds

Podfeeds is structured as an old-fashioned, Lynx-friendly website. There's a landing page at "/" with a link for each subscription.
Each of those links goes to a page. That page is an HTML rendering of the podcast's XML file, with a table of contents
at the top. Each entry at the table of contents is an episode.

You start on the list of podcasts you're subscribed to.

![Subscriptions](images/subscriptions.png)

Following each link gets you the page for that podcast. The page starts with anchor links to specific episodes.

![As It Happens](images/toc.png)

Use the table of contents to jump to episodes. Press Ctrl+A to jump back to the beginning of the page, where the table of contents is:

![As It Happens](images/episode.png)

Follow "Enclosure" links to play them.

![Playing](images/playing.png)

Or press "d" to download them or whatever; Lynx is very powerful.

## Troubleshooting

If you need to rebuild Podfeeds' internal page cache, then shut it down, delete cache.sqlite3,
and restart it.