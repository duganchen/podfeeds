# podfs
This is to be a FUSE filesystem for podcast subcriptions, written in Go.

Watch this space.

It will require you to supply podcast feed URIs. One way to find them is to
use the "podfeed" script I posted here:

[CLI app to get podcast feed URLs
](https://www.linuxquestions.org/questions/linuxquestions-org-member-success-stories-23/cli-app-to-get-podcast-feed-urls-4175656322/)

While this is still in progress, I can give you an idea of how it *will* work.

Podcast URLs will go in $XDG_CONFIG_HOME/podfeed/subscriptions.toml

You'll then mount the filesystem with "podfeed /path/to/podcasts".

Then /path/to/podcasts will contain a directory full of HTML files, each one being a podcast XML file
converted to HTML.

Updates, both edits to the subscription file and updates to the podcasts on their servers, will be handled transparently, and there will be no need
to umount and remount, or to take any other manual steps.