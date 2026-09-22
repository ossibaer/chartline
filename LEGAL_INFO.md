# Legal information

## Software license and third-party components

Chartline's original code and documentation are provided under the [MIT License](LICENSE). This license does not grant rights to music, recordings, artwork, third-party trademarks, or other content supplied by a host or user.

Third-party components retain their own licenses. The bundled Discord SDK's notices are in [DISCORD-LICENSE.txt](web/vendor/DISCORD-LICENSE.txt) and [THIRD-PARTY-LICENSES.txt](web/vendor/THIRD-PARTY-LICENSES.txt). Go dependencies are identified in [go.mod](go.mod); their respective license terms also apply. Preserve applicable notices when redistributing source or binaries.

## Independent project

Chartline is an independent music timeline game. It is not affiliated with, endorsed by, or sponsored by HITSTER or Jumbo. References to third-party products identify those products and do not imply permission to use their trademarks, artwork, rulebook text, software, or other protected materials. Third-party names and trademarks belong to their respective owners.

## Music and hosting

No music recordings are supplied with this repository. Hosts provide their own MP3 files and are responsible for obtaining the permissions required for their intended use, audience, and jurisdiction. Those permissions may concern both the musical composition and the particular recording, as well as copying, online transmission, and public playback.

Buying a recording or subscribing to a music service does not by itself grant permission to redistribute it or operate a public music service. Free access, a noncommercial purpose, or a software license does not replace the necessary music permissions. Use recordings you created and control all relevant rights to, or recordings whose licenses expressly allow your intended use, and comply with their conditions.

Chartline sends the full current song's audio payload to each participating browser for local playback. Ending a turn early does not limit the transfer to the excerpt heard. Authentication, room codes, and Discord allowlists restrict access but do not grant music rights or guarantee that a session is legally private. Players can capture the audio they receive.

The playback implementation strips ID3 tags to hide answers while leaving the original files intact. Before hosting licensed recordings, check whether their terms require attribution or preservation of rights information. Provide required credits and obtain permission or adjust the implementation where necessary; do not assume that revealing the artist and title after a turn satisfies every license.

Publishing the source code is separate from publishing or hosting recordings. Keep music files, credentials, and private deployment data out of public Git history, release archives, and other published artifacts. Use only appropriately cleared music in public demos and gameplay videos.

## External services

Discord integration is subject to Discord's applicable terms and developer policies; it does not supply a license for music playback. Chartline currently loads local MP3 files and does not integrate with Spotify. Any future music-service integration must comply with that service's terms: Spotify's [Developer Policy](https://developer.spotify.com/policy), for example, prohibits games, including trivia quizzes, as of the review date below.

## Legal context and further reading

Requirements differ by jurisdiction and by how the software is used. In Germany, [section 15 of the Copyright Act](https://www.gesetze-im-internet.de/urhg/__15.html) addresses public communication and personal relationships, and [section 53](https://www.gesetze-im-internet.de/urhg/__53.html) sets limits on private copying. A password-protected service is not automatically a private circle. [GEMA's online music guidance](https://www.gema.de/de/musiknutzer/musiknutzung-internet) provides a starting point for licensing enquiries; additional recording and other rights may need separate clearance.

This document provides general information, not legal advice or a guarantee of legal clearance. For a public or commercial deployment, obtain advice appropriate to your jurisdiction and the music and other materials you intend to use.

Last reviewed: 22 September 2026. External terms and laws may change.
