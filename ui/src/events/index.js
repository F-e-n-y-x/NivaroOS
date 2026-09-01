import {api}       from "@/service/service.js";
import message_bus from "@/events/message_bus.js";

export default function messageBus(name, params) {
	if (!params) {
		params = null
	}
	try {
		message_bus[name](params).then(res => {
			let properties = res.properties;
			let eventName = res.name;
			api.post(`/v2/message_bus/event/nivaroos-ui/${eventName}`, properties);
		})
	} catch (error) {
		console.log(error);
	}
}
