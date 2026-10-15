import { describe, expect, it } from 'vitest';
import { flipStep, hoverTurn, isImage } from './tile';

describe('hoverTurn', () => {
	it('turns with the pointer from where it came in, about a third of a turn across the tile', () => {
		expect(hoverTurn(-0.45, 100, 100, 200)).toBe(-0.45);
		expect(hoverTurn(0, 0, 200, 200)).toBeCloseTo(2);
		expect(hoverTurn(0, 200, 100, 200)).toBeCloseTo(-1);
	});
});

describe('flipStep', () => {
	it('turns one leaf at a time and stops on the back cover', () => {
		expect(flipStep(0, 6)).toBe(1);
		expect(flipStep(5, 6)).toBe(6);
		expect(flipStep(6, 6)).toBe(6);
	});
});

describe('isImage', () => {
	it('goes by the file extension, ignoring any query', () => {
		expect(isImage('https://cdn.example/minimagazine.gif')).toBe(true);
		expect(isImage('https://cdn.example/a.GIF?x=1')).toBe(true);
		expect(isImage('https://cdn.example/flip.mp4')).toBe(false);
	});
	it('counts photos, so a clip can be a still picture of someone wearing it', () => {
		expect(isImage('https://cdn.example/hack-club-red-tshirt-hq.jpg')).toBe(true);
		expect(isImage('https://cdn.example/a.jpeg')).toBe(true);
		expect(isImage('https://cdn.example/a.PNG')).toBe(true);
		expect(isImage('https://cdn.example/a.webp#x')).toBe(true);
	});
});
