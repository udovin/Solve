import React, {useContext} from "react";
import Page from "../layout/Page";
import Input from "../layout/Input";
import Button from "../layout/Button";
import {FormBlock} from "../layout/blocks";
import {Redirect} from "react-router";
import {AuthContext} from "../AuthContext";
import Field from "../layout/Field";

const LoginPage = () => {
	const {session, setSession} = useContext(AuthContext);
	let onSubmit = (event: any) => {
		event.preventDefault();
		const {login, password} = event.target;
		fetch("/api/v0/sessions", {
			method: "POST",
			headers: {
				"Content-Type": "application/json; charset=UTF-8",
			},
			body: JSON.stringify({
				Login: login.value,
				Password: password.value,
			})
		})
			.then(() => {
				fetch("/api/v0/sessions/current")
					.then(result => result.json())
					.then(result => setSession(result));
			});
	};
	if (session) {
		return <Redirect to={"/"}/>
	}
	return <Page title="Login">
		<FormBlock onSubmit={onSubmit} title="Login" footer={
			<Button type="submit" color="primary">Login</Button>
		}>
			<Field title="Username:">
				<Input type="text" name="login" placeholder="Username" required autoFocus/>
			</Field>
			<Field title="Password:">
				<Input type="password" name="password" placeholder="Password" required/>
			</Field>
		</FormBlock>
	</Page>;
};

export default LoginPage;
