import { describe, expect, it } from 'vitest';
import { programFor } from './programs';

describe('programFor', () => {
	it('links program posters and cards to their program', () => {
		expect(programFor('Pri/HPd/11x/1st')).toEqual({ name: 'Hackpad', href: 'https://hackpad.hackclub.com' });
		expect(programFor('Pri/HPd/4x6/1st')?.name).toBe('Hackpad');
		expect(programFor('Pri/Spr/11x/Lar')?.href).toBe('https://sprig.hackclub.com');
		expect(programFor('Pri/Bob/11x/Aco')?.href).toBe('https://boba.hackclub.com');
		expect(programFor('Pri/Sun/11x/Ban')?.href).toBe('https://sunbeam.hackclub.com');
	});

	it('links the magazines and their intro card to the magazine site', () => {
		expect(programFor('Pri/Bok/M25/1st')?.href).toBe('https://magazine.hackclub.com');
		expect(programFor('Pri/Bok/Mini25/1st')?.href).toBe('https://magazine.hackclub.com');
		expect(programFor('Pri/M25/4x6/1st')?.href).toBe('https://magazine.hackclub.com');
	});

	it('leaves general Hack Club swag alone', () => {
		expect(programFor('Swa/Shirt/HC')).toBeNull();
		expect(programFor('Sti/Bra/O&H/Lap')).toBeNull();
		expect(programFor('Pri/Wel/4x6/1st')).toBeNull();
	});
});
