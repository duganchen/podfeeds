# podfeeds
This is to be a podcast aggregator to use with [Lynx](https://lynx.invisible-island.net/)

Watch this space.

It will require you to supply podcast feed URIs. One way to find them is to
use the "podfeed" script I posted here:

[CLI app to get podcast feed URLs
](https://www.linuxquestions.org/questions/linuxquestions-org-member-success-stories-23/cli-app-to-get-podcast-feed-urls-4175656322/)

Paste them into a file in the root directory (same one with files like go.mod), named "podcasts.yaml". Separate each line with a
hyphen and a space. For example:

    - https://www.cbc.ca/podcasting/includes/wr.xml
    - https://www.cbc.ca/podcasting/includes/frontburner.xml

By default, the server will listen on port 80. Set the PORT environment variable to override it.

More to come....