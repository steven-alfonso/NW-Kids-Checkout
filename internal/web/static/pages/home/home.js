function escapeHtml(value) {
    return String(value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function departmentCardMarkup(group) {
    const name = group.name || '';
    const href = `/v1/checkins/checkouts?location_group_name=${encodeURIComponent(name)}&checked_out_after=-31m`;
    return `
        <a href="${href}"
           class="grade-card w-full md:w-5/12 bg-white rounded-lg p-6 shadow-[0_0_10px_rgba(0,0,0,0.25)] border border-gray-200">
            <div class="flex items-center md:flex-col md:items-center gap-4 md:gap-6">
                <div class="flex-shrink-0">
                    <div class="w-16 h-16 md:w-24 md:h-24 bg-sky-100 rounded-full flex items-center justify-center">
                        <div class="text-2xl md:text-4xl text-green-600">👦</div>
                    </div>
                </div>
                <div class="text-left md:text-center">
                    <div class="text-2xl md:text-4xl font-bold text-gray-800">${escapeHtml(name)}</div>
                </div>
            </div>
        </a>`;
}

async function loadLocationGroups() {
    const container = document.getElementById("department-cards");
    if (!container) {
        return;
    }
    let groups;
    try {
        const response = await fetch("/v1/location_groups");
        if (!response.ok) {
            return;
        }
        groups = await response.json();
    } catch (error) {
        return;
    }
    if (!groups || groups.length === 0) {
        return;
    }
    container.innerHTML = groups.map(departmentCardMarkup).join('');
}

document.addEventListener("DOMContentLoaded", () => {
    if (typeof window.initKebabMenu === "function") {
        window.initKebabMenu();
    } else if (window.NWKidsKebabMenu && typeof window.NWKidsKebabMenu.initKebabMenu === "function") {
        window.NWKidsKebabMenu.initKebabMenu();
    }
    loadLocationGroups();
});

window.__test = {
    loadLocationGroups,
    departmentCardMarkup,
};