package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.UserAreaDTO;
import org.cuit.epoch.dto.UserBackDTO;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.dto.UserInfoDTO;
import org.cuit.epoch.entity.UserAuth;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.PasswordVO;
import org.cuit.epoch.vo.UserVO;
import org.cuit.epoch.vo.page.PageResult;
import org.cuit.epoch.vo.strategy.login.WeiboLoginVO;

import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/11/15 21:44
 * @description:
 */
public interface UserAuthService extends IService<UserAuth> {


    /**
     * 参数：[]
     * 返回值：void
     * 作者： ladidol
     * 描述：登录接口
     */
    UserDetailDTO login(String username, String password);




    void logout();



    /**
     * 发送邮箱验证码
     *
     * @param username 邮箱号
     */
    void sendCode(String username);

    /**
     * 获取用户区域分布
     *
     * @param conditionVO 条件签证官
     * @return {@link List <UserAreaDTO>} 用户区域分布
     */
    List<UserAreaDTO> listUserAreas(ConditionVO conditionVO);

    /**
     * 用户注册
     *
     * @param user 用户对象
     */
    void register(UserVO user);

//    /**
//     * qq登录
//     *
//     * @param qqLoginVO qq登录信息
//     * @return 用户登录信息
//     */
//    UserInfoDTO qqLogin(QQLoginVO qqLoginVO);

    /**
     * 微博登录
     *
     * @param weiboLoginVO 微博登录信息
     * @return 用户登录信息
     */
    UserInfoDTO weiboLogin(WeiboLoginVO weiboLoginVO);

    /**
     * 修改密码
     *
     * @param user 用户对象
     */
    void updatePassword(UserVO user);

    /**
     * 修改管理员密码
     *
     * @param passwordVO 密码对象
     */
    void updateAdminPassword(PasswordVO passwordVO);

    /**
     * 查询后台用户列表
     *
     * @param condition 条件
     * @return 用户列表
     */
    PageResult<UserBackDTO> listUserBackDTO(ConditionVO condition);

}