// Kebab menu toggle. Menu links are rendered server-side (see
// internal/web/menu) so auth-gated routes are never shipped to clients in
// static source. This file only wires up open/close behavior.
const KEBAB_MENU_IDS = {
    button: "menu-button",
    menu: "kebab-menu",
};

function setupKebabMenu() {
    const menuButton = document.getElementById(KEBAB_MENU_IDS.button);
    const menu = document.getElementById(KEBAB_MENU_IDS.menu);

    if (!menuButton || !menu) {
        return;
    }

    // Prevent double-binding if init is called twice.
    if (menuButton.dataset.kebabBound === "true") {
        return;
    }
    menuButton.dataset.kebabBound = "true";

    function closeMenu() {
        menu.classList.add("hidden");
        menuButton.setAttribute("aria-expanded", "false");
    }

    function toggleMenu(event) {
        event.stopPropagation();
        const isOpen = !menu.classList.contains("hidden");
        if (isOpen) {
            closeMenu();
            return;
        }
        menu.classList.remove("hidden");
        menuButton.setAttribute("aria-expanded", "true");
    }

    menuButton.addEventListener("click", toggleMenu);
    menu.querySelectorAll("a").forEach((link) => {
        link.addEventListener("click", () => {
            closeMenu();
        });
    });
    document.addEventListener("click", (event) => {
        if (menu.classList.contains("hidden")) {
            return;
        }
        if (!menu.contains(event.target)) {
            closeMenu();
        }
    });
    document.addEventListener("keydown", (event) => {
        if (event.key === "Escape") {
            closeMenu();
        }
    });
}

function initKebabMenu() {
    setupKebabMenu();
}

if (typeof globalThis !== "undefined") {
    if (!globalThis.NWKidsKebabMenu) {
        globalThis.NWKidsKebabMenu = {};
    }
    globalThis.NWKidsKebabMenu.setupKebabMenu = setupKebabMenu;
    globalThis.NWKidsKebabMenu.initKebabMenu = initKebabMenu;
}

if (typeof window !== "undefined") {
    window.NWKidsKebabMenu = globalThis.NWKidsKebabMenu;
    window.setupKebabMenu = setupKebabMenu;
    window.initKebabMenu = initKebabMenu;
}

if (typeof module !== "undefined" && module.exports) {
    module.exports = {
        KEBAB_MENU_IDS,
        setupKebabMenu,
        initKebabMenu,
    };
}