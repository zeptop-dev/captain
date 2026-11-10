# City display corrections

The server information editor offers a searchable list of common cities. Selecting
one fills a manual country/region code and a complete manual map position in the
same form draft. It clears any old subdivision rather than carrying an unrelated
state into another country. Save explicitly to apply; cancel discards the draft.
The map fields remain editable for a specific datacenter or a city outside the
short list. Existing manual records are never replaced merely by opening the form.

The shared subscription-node location editor also offers these reference points.
It changes only that node's manual map position, not its hosting server's region.
An inherited position retains its inherited-source label until actually saved.

Read-only information and the server editor distinguish the saved effective map
position from the Country.is automatic result. Manual mode is labelled explicitly;
a country or known-city difference is informational, not a server failure. Old or
unconfirmed automatic data is labelled cached. Missing data is unknown, never
implied to confirm a manual city. A draft selection is not presented as saved.

These changes affect display metadata only. They do not alter location-IP selection,
public addresses, DNS, subscription endpoints, probes or routing. Automatic lookups
continue to be retained separately and cannot overwrite a manual map correction.
Country-only auto results are not interpreted as datacenter coordinates.

## City reference data and attribution

The 16-city convenience list in `web/admin/src/lib/location-cities.ts` is derived
from **GeoNames**, https://www.geonames.org/, distributed under **CC BY 4.0**:
https://creativecommons.org/licenses/by/4.0/.

Source: https://download.geonames.org/export/dump/cities15000.zip, retrieved
2026-11-10. Original GeoNames numeric IDs are retained. Names were selected for
common hosting locations, English display spellings were chosen, simplified
Chinese search labels added, coordinates rounded to four decimal places, and
administrative divisions omitted. These are approximate city reference points,
not verified facility coordinates and not a second IP geolocation database.
No browser geocoding or external search request is made by the city selector.
The downloaded source archive is not included in the repository.
