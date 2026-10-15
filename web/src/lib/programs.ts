/** The Hack Club program an item is for, so its product page can link there. */
export type Program = { name: string; href: string };

const PROGRAMS: { prefixes: string[]; program: Program }[] = [
	{ prefixes: ['Pri/HPd/'], program: { name: 'Hackpad', href: 'https://hackpad.hackclub.com' } },
	{ prefixes: ['Pri/Spr/'], program: { name: 'Sprig', href: 'https://sprig.hackclub.com' } },
	{ prefixes: ['Pri/Bob/'], program: { name: 'Boba Drops', href: 'https://boba.hackclub.com' } },
	{ prefixes: ['Pri/Sun/'], program: { name: 'Sunbeam', href: 'https://sunbeam.hackclub.com' } },
	{ prefixes: ['Pri/Bok/', 'Pri/M25/'], program: { name: 'Hack Club Magazine', href: 'https://magazine.hackclub.com' } },
	{ prefixes: ['Pri/Hac/Tea/'], program: { name: 'Hack Club for teachers', href: 'https://hackclub.com/teachers' } }
];

/** The program for a SKU, or null for general Hack Club swag. */
export function programFor(sku: string): Program | null {
	return PROGRAMS.find((p) => p.prefixes.some((prefix) => sku.startsWith(prefix)))?.program ?? null;
}
