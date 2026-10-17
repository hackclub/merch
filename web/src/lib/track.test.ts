import { describe, expect, it } from 'vitest';
import { cartChanges, describeClick, describeField, describeInvalid, nextVisit, PageClock, pageProps, SeenItems, sizeChanges, Tracker, vitalProps } from './track';

const el = (html: string) => {
	document.body.innerHTML = html;
	return document.body;
};

describe('Tracker', () => {
	it('batches events and stamps them with the visit and the client clock', () => {
		const sent: unknown[] = [];
		let now = 1000;
		const t = new Tracker({ send: (b) => sent.push(b), now: () => now, visit: () => 'v1', path: () => '/', release: 'r1' });
		t.track('page_view', { from: '' });
		now = 1500;
		t.track('click', { text: 'Add' }, '/items/3-x');
		expect(sent).toEqual([]);
		now = 2000;
		t.flush();
		expect(sent).toEqual([
			{
				visit: 'v1',
				release: 'r1',
				sent: 2000,
				events: [
					{ name: 'page_view', path: '/', t: 1000, props: { from: '' } },
					{ name: 'click', path: '/items/3-x', t: 1500, props: { text: 'Add' } }
				]
			}
		]);
		t.flush();
		expect(sent).toHaveLength(1); // nothing new
	});

	it('sends big queues in chunks', () => {
		const sent: { events: unknown[] }[] = [];
		const t = new Tracker({ send: (b) => sent.push(b as { events: unknown[] }), now: () => 1, visit: () => 'v', path: () => '/', release: '' });
		for (let i = 0; i < 95; i++) t.track('click');
		t.flush();
		expect(sent.map((b) => b.events.length)).toEqual([40, 40, 15]);
	});
});

describe('nextVisit', () => {
	it('keeps a visit going until 30 idle minutes', () => {
		const a = nextVisit(null, 0, () => 'new1');
		expect(a).toEqual({ id: 'new1', last: 0, fresh: true });
		expect(nextVisit({ id: 'new1', last: 0 }, 29 * 60_000, () => 'x')).toEqual({ id: 'new1', last: 29 * 60_000, fresh: false });
		expect(nextVisit({ id: 'new1', last: 0 }, 31 * 60_000, () => 'new2')).toEqual({ id: 'new2', last: 31 * 60_000, fresh: true });
	});
});

describe('cart changes', () => {
	it('lists what went up or down', () => {
		expect(cartChanges({ 1: 2, 2: 5 }, { 2: 10, 3: 1 })).toEqual([
			{ item_id: 1, from: 2, to: 0 },
			{ item_id: 2, from: 5, to: 10 },
			{ item_id: 3, from: 0, to: 1 }
		]);
		expect(cartChanges({ 1: 2 }, { 1: 2 })).toEqual([]);
	});

	it('lists size changes', () => {
		expect(sizeChanges({ 7: { 'Swa/S': 1 } }, { 7: { 'Swa/S': 1, 'Swa/M': 2 } })).toEqual([{ item_id: 7, sku: 'Swa/M', from: 0, to: 2 }]);
	});
});

describe('describeClick', () => {
	it('names the button, its item and its page', () => {
		el(`<div data-item="12"><button type="button" class="secondary">  Add
			</button></div>`);
		expect(describeClick(document.querySelector('button')!)).toEqual({ el: 'button', text: 'Add', item_id: 12 });
	});

	it('gives links their href and tags clicks on nothing clickable as dead', () => {
		el(`<a href="/items/3-orpheus-sticker"><img src="x.png" alt=""></a><p id="p">Words</p>`);
		expect(describeClick(document.querySelector('img')!)).toEqual({ el: 'a', target: 'img', href: '/items/3-orpheus-sticker' });
		expect(describeClick(document.getElementById('p')!)).toEqual({ el: 'p', dead: true });
	});

	it('leaves out text inside data-private and never reads input values', () => {
		el(`<ul data-private><li role="option">1 Main St, Burlington</li></ul><input name="city" value="Burlington">`);
		expect(describeClick(document.querySelector('li')!)).toEqual({ el: 'li', role: 'option' });
		expect(JSON.stringify(describeClick(document.querySelector('input')!))).not.toContain('Burlington');
	});
});

describe('describeField', () => {
	it('logs choices but only the length of typed text', () => {
		el(`<label>City <input autocomplete="address-level2" value="Burlington"></label>
			<input type="radio" name="purpose" value="club" checked>
			<label class="choice"><input type="checkbox" checked> <span>I understand</span></label>
			<select name="status"><option value="pending" selected>Pending</option></select>`);
		const [city, radio, box, select] = document.querySelectorAll('input, select');
		expect(describeField(city)).toEqual({ field: 'address-level2', type: 'text', chars: 10 });
		expect(describeField(radio)).toEqual({ field: 'purpose', type: 'radio', value: 'club' });
		expect(describeField(box)).toEqual({ field: 'I understand', type: 'checkbox', checked: true });
		expect(describeField(select)).toEqual({ field: 'status', type: 'select-one', value: 'pending' });
	});
});

describe('PageClock', () => {
	it('counts time while visible and recently used', () => {
		const c = new PageClock(0);
		c.tick(1000, true); // used just now
		c.tick(2000, true);
		expect(c.activeMs).toBe(2000);
		c.tick(3000, false); // tab hidden
		expect(c.activeMs).toBe(2000);
		c.input(3000);
		c.tick(4000, true);
		c.tick(4000 + 120_000, true); // idle for two minutes: counts until a minute after the last input
		expect(c.activeMs).toBe(3000 + 59_000);
		expect(c.visibleMs).toBe(3000 + 120_000);
	});
});

describe('pageProps', () => {
	it('picks out the item on product pages', () => {
		expect(pageProps('/items/12-orpheus-sticker')).toEqual({ item_id: 12 });
		expect(pageProps('/requests')).toEqual({});
	});
});

describe('SeenItems', () => {
	it('lists items that were at least half on screen for a second, in the order they were first seen', () => {
		const s = new SeenItems();
		s.update(5, true, 0);
		s.update(7, true, 0);
		s.update(7, false, 400); // scrolled past
		s.update(9, true, 500);
		expect(s.seen(1000)).toEqual([5]);
		s.update(7, true, 2000);
		expect(s.seen(3100)).toEqual([5, 9, 7]);
		s.update(9, false, 3200);
		expect(s.seen(9000)).toEqual([5, 9, 7]); // once seen, stays seen
	});
});

describe('describeInvalid', () => {
	it('names the field the browser refused and why, never its value', () => {
		el(`<form><input name="postal" required pattern="[0-9]{5}" value="ABCDE"><input type="email" name="email" value="orpheus"><input name="city" required></form>`);
		const [postal, email, city] = document.querySelectorAll('input');
		expect(describeInvalid(postal)).toEqual({ field: 'postal', type: 'text', reason: 'patternMismatch' });
		expect(describeInvalid(email)).toEqual({ field: 'email', type: 'email', reason: 'typeMismatch' });
		expect(describeInvalid(city)).toEqual({ field: 'city', type: 'text', reason: 'valueMissing' });
		expect(JSON.stringify(describeInvalid(postal))).not.toContain('ABCDE');
	});
});

describe('vitalProps', () => {
	it('keeps the metric, its value, rating and how the page was reached', () => {
		expect(vitalProps({ name: 'LCP', value: 1234.56, rating: 'good', navigationType: 'navigate' })).toEqual({
			metric: 'LCP', value: 1235, rating: 'good', nav: 'navigate'
		});
		expect(vitalProps({ name: 'CLS', value: 0.123456, rating: 'needs-improvement', navigationType: 'reload' })).toEqual({
			metric: 'CLS', value: 0.1235, rating: 'needs-improvement', nav: 'reload'
		});
	});
});
